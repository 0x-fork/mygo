package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// resourceProject writes a project with a resources directory and extra
// resources, and loads its config.
func resourceProject(t *testing.T, json string) *Config {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"mygo.json":                  json,
		"resources/icon.png":         string(defaultIcon()),
		"resources/data/words.txt":   "hello",
		"resources/data/.DS_Store":   "junk",
		"resources/.gitkeep":         "",
		"legal/NOTICE.txt":           "notice",
		"vendor/models/tiny.bin":     "model",
		"vendor/models/sub/more.bin": "more",
	})
	tool := filepath.Join(dir, "resources", "bin", "tool")
	writeFiles(t, dir, map[string]string{"resources/bin/tool": "#!/bin/sh\n"})
	if err := os.Chmod(tool, 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		// Listed entries are followed, links inside them are kept.
		if err := os.Symlink(filepath.Join("data", "words.txt"), filepath.Join(dir, "resources", "words")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("words.txt", filepath.Join(dir, "resources", "data", "alias")); err != nil {
			t.Fatal(err)
		}
	}
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestResources(t *testing.T) {
	c := resourceProject(t, `{"resources": ["legal/NOTICE.txt", "vendor/models"]}`)
	if c.Icon != filepath.Join("resources", "icon.png") {
		t.Errorf("icon = %q, want resources/icon.png by default", c.Icon)
	}
	list, err := c.resources(reservedNames(c, "darwin")...)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range list {
		names = append(names, r.name)
	}
	want := []string{"bin", "data", "icon.png", "NOTICE.txt", "models"}
	if runtime.GOOS != "windows" {
		want = []string{"bin", "data", "icon.png", "words", "NOTICE.txt", "models"}
	}
	if !slices.Equal(names, want) {
		t.Errorf("resources = %q, want %q", names, want)
	}

	dst := t.TempDir()
	if err := copyResources(list, dst); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"data/words.txt":      "hello",
		"NOTICE.txt":          "notice",
		"models/tiny.bin":     "model",
		"models/sub/more.bin": "more",
	} {
		if got, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(name))); err != nil || string(got) != content {
			t.Errorf("%s = %q, %v", name, got, err)
		}
	}
	for _, hidden := range []string{".gitkeep", "data/.DS_Store"} {
		if _, err := os.Lstat(filepath.Join(dst, filepath.FromSlash(hidden))); err == nil {
			t.Errorf("%s was copied", hidden)
		}
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(filepath.Join(dst, "bin", "tool")); err != nil || info.Mode().Perm()&0o111 == 0 {
			t.Errorf("bin/tool lost its executable bit: %v", err)
		}
		if info, err := os.Lstat(filepath.Join(dst, "words")); err != nil || !info.Mode().IsRegular() {
			t.Errorf("a listed link should be copied as a file: %v", err)
		}
		if link, err := os.Readlink(filepath.Join(dst, "data", "alias")); err != nil || link != "words.txt" {
			t.Errorf("a link inside a resource should stay a link: %q, %v", link, err)
		}
	}

	// Changes show in the hash, hidden files do not.
	hash := func() []byte {
		var b bytes.Buffer
		if err := hashResources(&b, list); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	before := hash()
	writeFiles(t, c.root, map[string]string{"resources/data/.DS_Store": "more junk"})
	if !bytes.Equal(before, hash()) {
		t.Error("a hidden file changed the hash")
	}
	writeFiles(t, c.root, map[string]string{"vendor/models/sub/more.bin": "changed"})
	if bytes.Equal(before, hash()) {
		t.Error("an edited resource did not change the hash")
	}

	// Copying a resource into itself would never end.
	if err := copyResources(list, filepath.Join(c.root, "vendor", "models", "out")); err == nil || !strings.Contains(err.Error(), "contains the directory") {
		t.Errorf("copying into a resource: %v", err)
	}
}

func TestResourceErrors(t *testing.T) {
	for _, tc := range []struct {
		json, reserved, want string
	}{
		{`{"resources": ["resources"]}`, "", "always included"},
		{`{"resources": ["./resources/"]}`, "", "always included"},
		{`{"resources": ["missing.txt"]}`, "", "missing.txt"},
		{`{"resources": ["legal/NOTICE.txt", "other/notice.txt"]}`, "", "would both be installed as notice.txt"},
		{`{"resources": ["vendor/data"]}`, "", "would both be installed as data"},
		{`{}`, "ICON.PNG", "would replace the app's own ICON.PNG"},
	} {
		c := resourceProject(t, tc.json)
		writeFiles(t, c.root, map[string]string{"other/notice.txt": "", "vendor/data/x": ""})
		var reserved []string
		if tc.reserved != "" {
			reserved = append(reserved, tc.reserved)
		}
		if _, err := c.resources(reserved...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.json, err, tc.want)
		}
	}

	// No resources at all, and a file where the directory belongs.
	c := &Config{root: t.TempDir()}
	if list, err := c.resources(); err != nil || len(list) != 0 {
		t.Errorf("no resources: %v, %v", list, err)
	}
	writeFiles(t, c.root, map[string]string{"resources": "not a directory"})
	if _, err := c.resources(); err == nil {
		t.Error("a resources file should fail")
	}

	names := func(goos string) []string { return reservedNames(&Config{Name: "My App"}, goos) }
	if got := names("darwin"); !slices.Equal(got, []string{"AppIcon.icns"}) {
		t.Errorf("darwin reserved %q", got)
	}
	if got := names("windows"); !slices.Equal(got, []string{"My App.exe"}) {
		t.Errorf("windows reserved %q", got)
	}
	if got := names("linux"); !slices.Equal(got, []string{"my-app", "my-app.desktop", "my-app.png"}) {
		t.Errorf("linux reserved %q", got)
	}
}

// TestPlaceBuild places a development build next to an earlier one.
func TestPlaceBuild(t *testing.T) {
	dir, stage := t.TempDir(), t.TempDir()
	src := t.TempDir()
	writeFiles(t, dir, map[string]string{"app": "old", "stale.txt": "", "kept/x": "", ".staging-1/y": ""})
	writeFiles(t, stage, map[string]string{"app": "new"})
	writeFiles(t, src, map[string]string{"kept/x": "x2", "fresh.txt": "fresh"})
	res := []resource{{"kept", filepath.Join(src, "kept")}, {"fresh.txt", filepath.Join(src, "fresh.txt")}}
	if err := placeBuild(stage, dir, res); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if want := []string{".staging-1", "app", "fresh.txt", "kept"}; !slices.Equal(got, want) {
		t.Errorf("dir = %q, want %q", got, want)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "app")); string(b) != "new" {
		t.Errorf("app = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "kept", "x")); string(b) != "x2" {
		t.Errorf("kept/x = %q", b)
	}
}

// thinMachO returns a Mach-O header followed by load commands of the given
// types: 64-bit little-endian, or 32-bit big-endian as on PowerPC.
func thinMachO(order binary.ByteOrder, cmds ...uint32) []byte {
	var b bytes.Buffer
	if order == binary.BigEndian {
		binary.Write(&b, order, [7]uint32{0xfeedface, 18, 0, 2, uint32(len(cmds)), uint32(len(cmds)) * 16, 0})
	} else {
		binary.Write(&b, order, [8]uint32{0xfeedfacf, 0x0100000c, 0, 2, uint32(len(cmds)), uint32(len(cmds)) * 16, 0, 0})
	}
	for _, cmd := range cmds {
		binary.Write(&b, order, [4]uint32{cmd, 16, 0, 0})
	}
	return b.Bytes()
}

func TestMachO(t *testing.T) {
	const segment, signature = 0x19, 0x1d
	le := binary.LittleEndian
	dir := t.TempDir()
	for name, head := range map[string][]byte{
		"thin":      thinMachO(le, segment),
		"signed":    thinMachO(le, segment, signature),
		"ppc":       thinMachO(binary.BigEndian, signature),
		"truncated": thinMachO(le, segment, signature)[:40],
		"header":    {0xcf, 0xfa, 0xed, 0xfe, 0x0c, 0, 0, 0x01},
		"class":     {0xca, 0xfe, 0xba, 0xbe, 0, 0, 0, 61},
		"text":      []byte("#!/bin/sh\necho"),
		"short":     {0xcf},
	} {
		if err := os.WriteFile(filepath.Join(dir, name), head, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, archs := range map[string][]string{"fat": {"signed", "signed"}, "mixed": {"signed", "thin"}} {
		for i := range archs {
			archs[i] = filepath.Join(dir, archs[i])
		}
		if err := writeUniversal(filepath.Join(dir, name), archs...); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string][2]bool{
		"thin": {true, false}, "signed": {true, true}, "ppc": {true, true}, "truncated": {true, false},
		"header": {true, false}, "fat": {true, true}, "mixed": {true, false},
		"class": {false, false}, "text": {false, false}, "short": {false, false},
	} {
		if ok, signed := machO(filepath.Join(dir, name)); ok != want[0] || signed != want[1] {
			t.Errorf("machO(%s) = %v, %v, want %v", name, ok, signed, want)
		}
	}
	if ok, signed := machO(filepath.Join(dir, "missing")); ok || signed {
		t.Error("a missing file is Mach-O")
	}
}

// TestSignNestedCode signs the code among the resources of an app ad hoc,
// from the inside out: code without a signature, code that
// macos.helperEntitlements lists, the bundles holding them, and bundles
// that are not sealed.
func TestSignNestedCode(t *testing.T) {
	if _, err := exec.LookPath("codesign"); err != nil || runtime.GOOS != "darwin" {
		t.Skip("needs codesign")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	app := filepath.Join(dir, "A.app")
	res := filepath.Join(app, "Contents", "Resources")
	sign := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("codesign", args...).CombinedOutput(); err != nil {
			t.Fatalf("codesign %q: %v\n%s", args, err, out)
		}
	}
	for _, name := range []string{"MacOS/A", "Resources/bin/signed", "Resources/bin/unsigned", "Resources/bin/jit",
		"Resources/Helper.app/Contents/MacOS/Helper", "Resources/Helper.app/Contents/Resources/tool"} {
		path := filepath.Join(app, "Contents", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := copyFile(exe, path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	plist := func(key string) string {
		return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>` + key + `</key><true/></dict></plist>
`
	}
	writeFiles(t, dir, map[string]string{
		"A.app/Contents/Info.plist":                                          string(infoPlist(&Config{Name: "A", Identifier: "com.example.a"}, "A", "")),
		"A.app/Contents/Resources/Helper.app/Contents/Info.plist":            string(infoPlist(&Config{Name: "Helper", Identifier: "com.example.helper"}, "Helper", "")),
		"A.app/Contents/Resources/Docs.bundle/Contents/Info.plist":           string(infoPlist(&Config{Name: "Docs", Identifier: "com.example.docs"}, "Docs", "")),
		"A.app/Contents/Resources/Docs.bundle/Contents/Resources/index.html": "<h1>Docs</h1>",
		"A.app/Contents/Resources/data.txt":                                  "data",
		"jit.plist":                                                          plist("com.apple.security.cs.allow-jit"),
		"helper.plist":                                                       plist("com.apple.security.cs.disable-library-validation"),
	})
	// A framework built without sealing it: its library is signed alone.
	framework := filepath.Join(res, "Foo.framework")
	writeFiles(t, framework, map[string]string{
		"Versions/A/Resources/Info.plist": string(infoPlist(&Config{Name: "Foo", Identifier: "com.example.foo"}, "Foo", "")),
	})
	library := filepath.Join(dir, "Foo")
	if err := copyFile(exe, library, 0o755); err != nil {
		t.Fatal(err)
	}
	sign("--force", "--sign", "-", library)
	if err := os.Rename(library, filepath.Join(framework, "Versions", "A", "Foo")); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{"Versions/Current": "A", "Foo": "Versions/Current/Foo", "Resources": "Versions/Current/Resources"} {
		if err := os.Symlink(target, filepath.Join(framework, filepath.FromSlash(link))); err != nil {
			t.Fatal(err)
		}
	}
	helper := filepath.Join(res, "Helper.app")
	sign("--remove-signature", filepath.Join(res, "bin", "unsigned"))
	sign("--remove-signature", filepath.Join(helper, "Contents", "Resources", "tool"))
	sign("--force", "--sign", "-", "--identifier", "com.example.kept", filepath.Join(res, "bin", "signed"))
	sign("--force", "--sign", "-", "--entitlements", filepath.Join(dir, "helper.plist"), helper)

	c := &Config{root: dir, MacOS: MacOS{HelperEntitlements: map[string]string{"./bin/jit": "jit.plist"}}}
	if err := codesign(c, app, "-", false); err != nil {
		t.Fatal(err)
	}
	details := func(path string, args ...string) string {
		out, err := exec.Command("codesign", append(append([]string{"--display"}, args...), path)...).CombinedOutput()
		if err != nil {
			t.Errorf("codesign --display %s: %v\n%s", path, err, out)
		}
		return string(out)
	}
	for _, path := range []string{app, helper, framework, filepath.Join(res, "bin", "unsigned"), filepath.Join(helper, "Contents", "Resources", "tool")} {
		if out, err := exec.Command("codesign", "--verify", "--deep", "--strict", path).CombinedOutput(); err != nil {
			rel, _ := filepath.Rel(dir, path)
			t.Errorf("%s does not verify: %v\n%s", rel, err, out)
		}
	}
	if out := details(filepath.Join(res, "bin", "signed"), "--verbose"); !strings.Contains(out, "Identifier=com.example.kept") {
		t.Errorf("code with a signature was signed again:\n%s", out)
	}
	if out := details(filepath.Join(res, "bin", "jit"), "--entitlements", "-", "--xml"); !strings.Contains(out, "allow-jit") {
		t.Errorf("bin/jit lacks the entitlements of macos.helperEntitlements:\n%s", out)
	}
	if out := details(helper, "--entitlements", "-", "--xml"); !strings.Contains(out, "disable-library-validation") {
		t.Errorf("the helper app lost its entitlements:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(res, "Docs.bundle", "Contents", "_CodeSignature")); err == nil {
		t.Error("a bundle without code was signed")
	}

	c.MacOS.HelperEntitlements = map[string]string{"bin/missing": "jit.plist", "data.txt": "jit.plist"}
	if err := codesign(c, app, "-", false); err == nil || !strings.Contains(err.Error(), "no executable, library or bundle at bin/missing, data.txt") {
		t.Errorf("unknown helpers: %v", err)
	}
}

func TestNestedCodesignArgs(t *testing.T) {
	for _, tc := range []struct {
		identity, entitlements string
		production             bool
		want                   []string
	}{
		{"-", "", true, []string{"--force", "--sign", "-", "--preserve-metadata=entitlements"}},
		{"Developer ID Application: X", "", true, []string{"--force", "--sign", "Developer ID Application: X", "--options", "runtime", "--timestamp", "--preserve-metadata=entitlements"}},
		{"Developer ID Application: X", "e.plist", true, []string{"--force", "--sign", "Developer ID Application: X", "--options", "runtime", "--timestamp", "--entitlements", "e.plist"}},
		{"Apple Development: X", "", false, []string{"--force", "--sign", "Apple Development: X", "--preserve-metadata=entitlements"}},
	} {
		if got := nestedCodesignArgs(tc.identity, tc.entitlements, tc.production); !slices.Equal(got, tc.want) {
			t.Errorf("nestedCodesignArgs(%q, %q, %v) = %q", tc.identity, tc.entitlements, tc.production, got)
		}
	}
}

// TestBuildResources builds an app for each platform and finds its
// resources where the app looks for them, then builds again without one.
func TestBuildResources(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles programs")
	}
	dir := testModule(t, map[string]string{
		"main.go":              "package main\n\nimport \"github.com/egoist/mygo\"\n\nfunc main() { mygo.App.Run() }\n",
		"mygo.json":            `{"name": "Res App", "resources": ["extra.txt"]}`,
		"resources/data/a.txt": "a",
		"resources/icon.png":   string(defaultIcon()),
		"extra.txt":            "extra",
	})
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	opts := buildOptions{sign: "-", skipDMG: true, work: t.TempDir()}
	if opts.pkg, err = packageDir(c); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ goos, goarch, resources string }{
		{"darwin", "arm64", "darwin-arm64/Res App.app/Contents/Resources"},
		{"linux", "amd64", "linux-amd64"},
		{"windows", "amd64", "windows-amd64"},
	} {
		if _, err := buildPlatform(c, tc.goos, tc.goarch, opts); err != nil {
			t.Fatalf("%s: %v", tc.goos, err)
		}
		res := filepath.Join(dir, "dist", filepath.FromSlash(tc.resources))
		for name, content := range map[string]string{"data/a.txt": "a", "extra.txt": "extra"} {
			if b, err := os.ReadFile(filepath.Join(res, filepath.FromSlash(name))); err != nil || string(b) != content {
				t.Errorf("%s: %s = %q, %v", tc.goos, name, b, err)
			}
		}
		if tc.goos == "darwin" {
			if _, err := os.Stat(filepath.Join(res, "AppIcon.icns")); err != nil {
				t.Errorf("darwin: no icon: %v", err)
			}
		}
		if tc.goos == "windows" {
			if image, signed := peImage(filepath.Join(res, "Res App.exe")); !image || signed {
				t.Errorf("windows: peImage = %v, %v for the unsigned executable", image, signed)
			}
		}
		if info, err := os.Stat(filepath.Join(dir, "dist", tc.goos+"-"+tc.goarch)); err != nil {
			t.Error(err)
		} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
			t.Errorf("%s: output directory mode %v", tc.goos, info.Mode())
		}
	}

	// A rebuild leaves nothing stale behind.
	if err := os.Remove(filepath.Join(dir, "resources", "data", "a.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := buildPlatform(c, "linux", "amd64", opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dist", "linux-amd64", "data", "a.txt")); err == nil {
		t.Error("a removed resource is still in the build")
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "dist"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("left behind: %s", e.Name())
		}
	}
}
