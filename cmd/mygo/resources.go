package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// resourcesDir is the project directory whose contents ship with the app:
// resources/data/words.txt is installed as data/words.txt in the app's
// resource directory (mygo.PathResources), which is Contents/Resources in a
// macOS bundle and the executable's directory elsewhere.
const resourcesDir = "resources"

// bundleIcon is the file name of the icon in Contents/Resources.
const bundleIcon = "AppIcon.icns"

// resource is a file or directory installed as name in the resource
// directory.
type resource struct {
	name string
	src  string
}

// resources lists what the app ships with: the entries of the project's
// resources directory, then the extra resources of mygo.json under their
// base names. Names starting with a dot are left out of directories.
// Destination names must be unique, ignoring case as macOS and Windows do,
// and differ from reserved, the files the packaging adds itself.
func (c *Config) resources(reserved ...string) ([]resource, error) {
	own := map[string]string{} // by lower-case name, like taken
	for _, name := range reserved {
		own[strings.ToLower(name)] = name
	}
	taken := map[string]string{}
	var list []resource
	add := func(name, src string) error {
		rel, err := filepath.Rel(c.root, src)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = src
		}
		key := strings.ToLower(name)
		if file, ok := own[key]; ok {
			return fmt.Errorf("resource %s would replace the app's own %s", rel, file)
		}
		if prev, ok := taken[key]; ok {
			return fmt.Errorf("resources %s and %s would both be installed as %s", prev, rel, name)
		}
		taken[key] = rel
		list = append(list, resource{name, src})
		return nil
	}

	dir := c.path(resourcesDir)
	var entries []os.DirEntry
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	} else if err == nil {
		if entries, err = os.ReadDir(dir); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, e := range entries {
		if !hiddenName(e.Name()) {
			if err := add(e.Name(), filepath.Join(dir, e.Name())); err != nil {
				return nil, err
			}
		}
	}
	for _, p := range c.Resources {
		src := c.path(p)
		if filepath.Clean(src) == dir {
			return nil, fmt.Errorf("%s: resources lists %s, whose contents are always included; list only extra files", c.configName(), p)
		}
		if _, err := os.Stat(src); err != nil {
			return nil, fmt.Errorf("%s: resource %s: %w", c.configName(), p, err)
		}
		if err := add(filepath.Base(src), src); err != nil {
			return nil, err
		}
	}
	return list, nil
}

// reservedNames are the files the packaging puts in the resource directory
// of an app for goos, which resources must not replace.
func reservedNames(c *Config, goos string) []string {
	switch goos {
	case "darwin":
		return []string{bundleIcon}
	case "windows":
		return []string{c.executableName() + ".exe"}
	}
	name := slugify(c.executableName())
	return []string{name, name + ".desktop", name + ".png"}
}

func hiddenName(name string) bool { return strings.HasPrefix(name, ".") }

// copyResources copies list into dir.
func copyResources(list []resource, dir string) error {
	for _, r := range list {
		if within(dir, r.src) {
			return fmt.Errorf("resource %s contains the directory it is copied to", r.src)
		}
		if err := copyResource(r.src, filepath.Join(dir, r.name)); err != nil {
			return err
		}
	}
	return nil
}

// copyResource copies a file or directory to dst like `cp -RH`: src itself
// is followed when it is a symbolic link, links inside it are copied as
// links. Permissions are kept, so executables stay executable.
func copyResource(src, dst string) error {
	return walkResource(src, func(path string, info fs.FileInfo) error {
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch mode := info.Mode(); {
		case mode&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case mode.IsDir():
			return os.MkdirAll(target, 0o755)
		case mode.IsRegular():
			return copyFile(path, target, mode.Perm())
		}
		return fmt.Errorf("resource %s is not a regular file", path)
	})
}

// walkResource calls fn for src and, when it is a directory, everything in
// it, in the order copyResource copies them.
func walkResource(src string, fn func(path string, info fs.FileInfo) error) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	var walk func(path string, info fs.FileInfo) error
	walk = func(path string, info fs.FileInfo) error {
		if err := fn(path, info); err != nil || !info.IsDir() {
			return err
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if hiddenName(e.Name()) {
				continue
			}
			child, err := e.Info() // links are not followed
			if err != nil {
				return err
			}
			if err := walk(filepath.Join(path, e.Name()), child); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(src, info)
}

// hashResources writes what list is made of to w (names, permissions,
// sizes and modification times), to tell whether it changed.
func hashResources(w io.Writer, list []resource) error {
	for _, r := range list {
		err := walkResource(r.src, func(path string, info fs.FileInfo) error {
			rel, _ := filepath.Rel(r.src, path)
			fmt.Fprintf(w, "%s\x00%s\x00%v\x00", r.name, rel, info.Mode())
			if !info.IsDir() { // a directory's time changes with hidden files too
				fmt.Fprintf(w, "%d\x00%d\x00", info.Size(), info.ModTime().UnixNano())
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	resolve := func(p string) string {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		return p
	}
	rel, err := filepath.Rel(resolve(dir), resolve(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// signNestedCode signs the code among the resources of a bundle, which
// codesign --deep leaves alone: it only signs the bundle's code
// directories, and notarization rejects unsigned code anywhere. Code is
// signed from the inside out: Mach-O files, then the bundles holding them
// (apps, frameworks, plug-ins), whose signatures seal what they contain. A
// real identity signs all of it. Ad hoc signing only signs what has no
// valid signature, so that copies keep their own: Mach-O files without
// one, which Apple silicon does not run, bundles that are not sealed, and
// the bundles around what it signed. Code keeps the entitlements it is
// signed with, unless macos.helperEntitlements gives it others.
func signNestedCode(c *Config, app, identity string, production bool) error {
	dir := filepath.Join(app, "Contents", "Resources")
	type nested struct {
		path, name string // name: the path in dir, with slashes
		bundle     bool
		signed     bool // a Mach-O file with a signature
	}
	var files, bundles []nested
	err := walkResource(dir, func(path string, info fs.FileInfo) error {
		rel, _ := filepath.Rel(dir, path)
		n := nested{path: path, name: filepath.ToSlash(rel), bundle: info.IsDir()}
		switch {
		case info.Mode().IsRegular():
			var ok bool
			if ok, n.signed = machO(path); ok {
				files = append(files, n)
			}
		case info.IsDir() && isBundle(path):
			bundles = append(bundles, n)
		}
		return nil
	})
	if err != nil {
		return err
	}
	inside := func(bundle string, list []nested) bool {
		return slices.ContainsFunc(list, func(n nested) bool { return strings.HasPrefix(n.name, bundle+"/") })
	}
	list := slices.Clone(files)
	for _, b := range bundles {
		// Bundles without code, such as localizations, are resources.
		if inside(b.name, files) {
			list = append(list, b)
		}
	}
	// What is deeper comes first, so a bundle comes after its contents.
	slices.SortStableFunc(list, func(a, b nested) int {
		return strings.Count(b.name, "/") - strings.Count(a.name, "/")
	})

	entitlements := map[string]string{}
	var unknown []string
	for name, file := range c.MacOS.HelperEntitlements {
		name = filepath.ToSlash(filepath.Clean(name))
		entitlements[name] = c.path(file)
		if !slices.ContainsFunc(list, func(n nested) bool { return n.name == name }) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return fmt.Errorf("%s: macos.helperEntitlements: the resources hold no executable, library or bundle at %s", c.configName(), strings.Join(unknown, ", "))
	}

	var signed []nested
	for _, n := range list {
		ent := entitlements[n.name]
		if identity == "-" && ent == "" {
			switch {
			case !n.bundle && n.signed:
				continue
			case n.bundle && !inside(n.name, signed):
				if exec.Command("codesign", "--verify", n.path).Run() == nil {
					continue
				}
			}
		}
		args := nestedCodesignArgs(identity, ent, production)
		if out, err := exec.Command("codesign", append(args, n.path)...).CombinedOutput(); err != nil {
			return fmt.Errorf("codesign %s: %v\n%s", n.name, err, out)
		}
		signed = append(signed, n)
	}
	return nil
}

// nestedCodesignArgs returns the codesign arguments, before the path, that
// sign code among the resources like the app: with the hardened runtime and
// a secure timestamp for real identities in production. The code keeps the
// entitlements it is signed with, such as the JIT of a JavaScript runtime,
// unless entitlements names a property list of others.
func nestedCodesignArgs(identity, entitlements string, production bool) []string {
	args := []string{"--force", "--sign", identity}
	if production && identity != "-" {
		args = append(args, "--options", "runtime", "--timestamp")
	}
	if entitlements != "" {
		return append(args, "--entitlements", entitlements)
	}
	return append(args, "--preserve-metadata=entitlements")
}

// isBundle reports whether dir is a bundle that can hold code: an app, a
// framework or a plug-in, with an Info.plist where codesign looks for it.
func isBundle(dir string) bool {
	switch strings.ToLower(filepath.Ext(dir)) {
	case ".app", ".appex", ".bundle", ".framework", ".plugin", ".xpc":
	default:
		return false
	}
	for _, plist := range []string{"Contents/Info.plist", "Resources/Info.plist", "Info.plist"} {
		if fileExists(filepath.Join(dir, filepath.FromSlash(plist))) {
			return true
		}
	}
	return false
}

// machO reports whether the file at path is a Mach-O file or a universal
// binary, and whether every architecture in it has a code signature.
func machO(path string) (ok, signed bool) {
	f, err := os.Open(path)
	if err != nil {
		return false, false
	}
	defer f.Close()
	var head [8]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return false, false
	}
	magic := binary.BigEndian.Uint32(head[:])
	switch magic {
	case 0xfeedface, 0xfeedfacf, 0xcefaedfe, 0xcffaedfe:
		return true, machOSigned(f, 0)
	case 0xcafebabe, 0xcafebabf:
	default:
		return false, false
	}
	n := binary.BigEndian.Uint32(head[4:])
	if magic == 0xcafebabe && n >= 20 {
		// Java class files share the magic; their version follows it, a
		// much larger number than the architectures of a universal binary.
		return false, false
	}
	size := int64(20) // fat_arch
	if magic == 0xcafebabf {
		size = 32 // fat_arch_64
	}
	signed = n > 0
	for i := range int64(n) {
		var arch [32]byte
		if _, err := f.ReadAt(arch[:size], 8+i*size); err != nil {
			return true, false
		}
		offset := int64(binary.BigEndian.Uint32(arch[8:]))
		if magic == 0xcafebabf {
			offset = int64(binary.BigEndian.Uint64(arch[8:]))
		}
		signed = signed && machOSigned(f, offset)
	}
	return true, signed
}

// machOSigned reports whether the Mach-O file at offset in r has a code
// signature, an LC_CODE_SIGNATURE load command.
func machOSigned(r io.ReaderAt, offset int64) bool {
	var h [28]byte // mach_header; mach_header_64 adds a reserved field
	if _, err := r.ReadAt(h[:], offset); err != nil {
		return false
	}
	var order binary.ByteOrder = binary.LittleEndian
	magic := order.Uint32(h[:])
	if magic == 0xcefaedfe || magic == 0xcffaedfe {
		order, magic = binary.BigEndian, binary.BigEndian.Uint32(h[:])
	}
	size := int64(28)
	switch magic {
	case 0xfeedface:
	case 0xfeedfacf:
		size = 32
	default:
		return false
	}
	ncmds, sizeofcmds := order.Uint32(h[16:]), order.Uint32(h[20:])
	if sizeofcmds > 1<<24 {
		return false
	}
	cmds := make([]byte, sizeofcmds)
	if _, err := r.ReadAt(cmds, offset+size); err != nil {
		return false
	}
	for range ncmds {
		if len(cmds) < 8 {
			break
		}
		cmd, cmdsize := order.Uint32(cmds), order.Uint32(cmds[4:])
		if cmd == 0x1d { // LC_CODE_SIGNATURE
			return true
		}
		if cmdsize < 8 || uint64(cmdsize) > uint64(len(cmds)) {
			break
		}
		cmds = cmds[cmdsize:]
	}
	return false
}
