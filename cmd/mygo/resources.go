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
			return nil, fmt.Errorf("mygo.json: resources lists %s, whose contents are always included; list only extra files", p)
		}
		if _, err := os.Stat(src); err != nil {
			return nil, fmt.Errorf("mygo.json: resource %s: %w", p, err)
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

// signNestedCode signs the Mach-O executables and libraries among the
// resources of a bundle, which codesign --deep leaves alone: it only signs
// the bundle's code directories, and notarization rejects unsigned code
// anywhere. Ad hoc signing leaves the copies with their own signatures.
func signNestedCode(app, identity string, production bool) error {
	if identity == "-" {
		return nil
	}
	var code []string
	err := walkResource(filepath.Join(app, "Contents", "Resources"), func(path string, info fs.FileInfo) error {
		if info.Mode().IsRegular() && isMachO(path) {
			code = append(code, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, path := range code {
		args := []string{"--force", "--sign", identity}
		if production {
			args = append(args, "--options", "runtime", "--timestamp")
		}
		if out, err := exec.Command("codesign", append(args, path)...).CombinedOutput(); err != nil {
			rel, _ := filepath.Rel(app, path)
			return fmt.Errorf("codesign %s: %v\n%s", rel, err, out)
		}
	}
	return nil
}

// isMachO reports whether the file at path is a Mach-O file or a universal
// binary.
func isMachO(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var head [8]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return false
	}
	switch binary.BigEndian.Uint32(head[:]) {
	case 0xfeedface, 0xfeedfacf, 0xcefaedfe, 0xcffaedfe, 0xcafebabf:
		return true
	case 0xcafebabe:
		// Java class files share the magic; their version follows it, a
		// much larger number than the architectures of a universal binary.
		return binary.BigEndian.Uint32(head[4:]) < 20
	}
	return false
}
