package main

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// writeBundle assembles <dir>/<executable>.app around the executable bin,
// which is moved into it, with the icon icns (or nil) and the resources res,
// and returns the bundle path.
func writeBundle(c *Config, dir, bin string, icns []byte, res []resource) (string, error) {
	name := c.executableName()
	app := filepath.Join(dir, name+".app")
	if err := os.RemoveAll(app); err != nil {
		return "", err
	}
	contents := filepath.Join(app, "Contents")
	for _, d := range []string{"MacOS", "Resources"} {
		if err := os.MkdirAll(filepath.Join(contents, d), 0o755); err != nil {
			return "", err
		}
	}
	if err := os.Rename(bin, filepath.Join(contents, "MacOS", name)); err != nil {
		return "", err
	}
	iconFile := ""
	if icns != nil {
		iconFile = bundleIcon
		if err := os.WriteFile(filepath.Join(contents, "Resources", iconFile), icns, 0o644); err != nil {
			return "", err
		}
	}
	if err := copyResources(res, filepath.Join(contents, "Resources")); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), infoPlist(c, name, iconFile), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(contents, "PkgInfo"), []byte("APPL????"), 0o644); err != nil {
		return "", err
	}
	return app, nil
}

// bundleExecutable returns the path of the executable inside a bundle.
func bundleExecutable(app string) string {
	name := filepath.Base(app)
	return filepath.Join(app, "Contents", "MacOS", name[:len(name)-len(".app")])
}

// appIcon renders the configured icon as .icns, or returns nil without one.
func appIcon(c *Config) ([]byte, error) {
	if c.Icon == "" {
		return nil, nil
	}
	src, err := os.ReadFile(c.path(c.Icon))
	if err != nil {
		return nil, err
	}
	icns, err := pngToICNS(src)
	if err != nil {
		return nil, fmt.Errorf("icon %s: %w", c.Icon, err)
	}
	return icns, nil
}

func infoPlist(c *Config, executable, icon string) []byte {
	var b bytes.Buffer
	esc := func(s string) string {
		var e bytes.Buffer
		_ = xml.EscapeText(&e, []byte(s))
		return e.String()
	}
	kv := func(k, v string) { fmt.Fprintf(&b, "\t<key>%s</key>\n\t<string>%s</string>\n", k, esc(v)) }
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	kv("CFBundleName", c.Name)
	kv("CFBundleDisplayName", c.Name)
	kv("CFBundleIdentifier", c.Identifier)
	kv("CFBundleVersion", c.Version)
	kv("CFBundleShortVersionString", c.Version)
	kv("CFBundleExecutable", executable)
	kv("CFBundlePackageType", "APPL")
	kv("CFBundleInfoDictionaryVersion", "6.0")
	kv("LSMinimumSystemVersion", c.MacOS.MinimumSystemVersion)
	kv("NSPrincipalClass", "NSApplication")
	if icon != "" {
		kv("CFBundleIconFile", icon)
	}
	if c.Copyright != "" {
		kv("NSHumanReadableCopyright", c.Copyright)
	}
	b.WriteString("\t<key>NSHighResolutionCapable</key>\n\t<true/>\n")
	b.WriteString("\t<key>NSSupportsAutomaticGraphicsSwitching</key>\n\t<true/>\n")
	if len(c.URLSchemes) > 0 {
		b.WriteString("\t<key>CFBundleURLTypes</key>\n\t<array>\n\t\t<dict>\n")
		fmt.Fprintf(&b, "\t\t\t<key>CFBundleURLName</key>\n\t\t\t<string>%s</string>\n", esc(c.Identifier))
		b.WriteString("\t\t\t<key>CFBundleURLSchemes</key>\n\t\t\t<array>\n")
		for _, s := range c.URLSchemes {
			fmt.Fprintf(&b, "\t\t\t\t<string>%s</string>\n", esc(s))
		}
		b.WriteString("\t\t\t</array>\n\t\t</dict>\n\t</array>\n")
	}
	b.WriteString("</dict>\n</plist>\n")
	return b.Bytes()
}

// codesignArgs returns the codesign arguments that sign path. Real
// identities get the hardened runtime and a secure timestamp in production,
// which notarization requires.
func codesignArgs(path, identity, entitlements string, production bool) []string {
	args := []string{"--force", "--deep", "--sign", identity}
	if production && identity != "-" {
		args = append(args, "--options", "runtime", "--timestamp")
	}
	if entitlements != "" {
		args = append(args, "--entitlements", entitlements)
	}
	return append(args, path)
}

// codesign signs a bundle, and the code among its resources. Signing needs
// macOS: elsewhere only the executable's ad-hoc signature from the Go linker
// remains.
func codesign(c *Config, path, identity string, production bool) error {
	if runtime.GOOS != "darwin" {
		if identity != "-" {
			logf("not signing %s: code signing needs macOS", filepath.Base(path))
		}
		return nil
	}
	if err := signNestedCode(path, identity, production); err != nil {
		return err
	}
	entitlements := ""
	if c.MacOS.Entitlements != "" {
		entitlements = c.path(c.MacOS.Entitlements)
	}
	out, err := exec.Command("codesign", codesignArgs(path, identity, entitlements, production)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesign %s: %v\n%s", filepath.Base(path), err, out)
	}
	return nil
}

// replacePath moves src to dst, replacing what dst held. The old dst is
// renamed away before it is removed, so a running app keeps its files.
// Removing it is best effort: Windows keeps running executables.
func replacePath(src, dst string) error {
	old := ""
	if _, err := os.Lstat(dst); err == nil {
		old = filepath.Join(filepath.Dir(dst), fmt.Sprintf(".old-%d-%s", os.Getpid(), filepath.Base(dst)))
		_ = os.RemoveAll(old)
		if err := os.Rename(dst, old); err != nil {
			return err
		}
	}
	if err := os.Rename(src, dst); err != nil {
		if old != "" {
			_ = os.Rename(old, dst)
		}
		return err
	}
	if old != "" {
		_ = os.RemoveAll(old)
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// writeUniversal combines Mach-O executables into a universal (fat)
// binary, like lipo, so no Xcode tools are needed.
func writeUniversal(out string, slices ...string) error {
	const (
		fatMagic = 0xcafebabe
		align    = 14 // 2^14 = 16 KiB
	)
	type arch struct {
		cpuType, cpuSubtype uint32
		data                []byte
	}
	var archs []arch
	for _, p := range slices {
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if len(data) < 12 || binary.LittleEndian.Uint32(data) != 0xfeedfacf {
			return fmt.Errorf("%s is not a 64-bit Mach-O file", p)
		}
		archs = append(archs, arch{binary.LittleEndian.Uint32(data[4:]), binary.LittleEndian.Uint32(data[8:]), data})
	}
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, [2]uint32{fatMagic, uint32(len(archs))})
	offset := uint32(1 << align)
	offsets := make([]uint32, len(archs))
	for i, a := range archs {
		offsets[i] = offset
		_ = binary.Write(&buf, binary.BigEndian, [5]uint32{a.cpuType, a.cpuSubtype, offset, uint32(len(a.data)), align})
		offset += (uint32(len(a.data)) + (1<<align - 1)) &^ (1<<align - 1)
	}
	for i, a := range archs {
		buf.Write(make([]byte, int(offsets[i])-buf.Len()))
		buf.Write(a.data)
	}
	return os.WriteFile(out, buf.Bytes(), 0o755)
}
