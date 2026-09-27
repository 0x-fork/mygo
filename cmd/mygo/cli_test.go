package main

import (
	"bytes"
	"encoding/binary"
	"go/parser"
	"go/token"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "My App")
	if err := os.MkdirAll(filepath.Join(dir, "frontend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frontend", "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "My App" || c.Identifier != "com.mygo.myapp" || c.Version != "0.1.0" || c.Main != "." {
		t.Errorf("unexpected defaults: %+v", c)
	}
	if c.DevURL != "" || c.DevCommand != "" || c.BuildCommand != "" || c.FrontendDist != "" {
		t.Errorf("frontend options are opt-in: %+v", c)
	}
	if c.Bindings != filepath.Join("frontend", "src", "mygo.ts") {
		t.Errorf("bindings = %q", c.Bindings)
	}

	write := func(json string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "mygo.json"), []byte(json), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"name":"Custom","bindings":"web/api.ts","devUrl":"http://localhost:3000","devCommand":"bun run dev","buildCommand":"bun run build","frontendDist":"web/dist"}`)
	c, err = loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Custom" || c.Bindings != "web/api.ts" || c.DevURL != "http://localhost:3000" || c.DevCommand != "bun run dev" || c.BuildCommand != "bun run build" || c.FrontendDist != "web/dist" {
		t.Errorf("mygo.json not applied: %+v", c)
	}
	write(`{"devUrl":"localhost:3000"}`)
	if _, err := loadConfig(dir); err == nil || !strings.Contains(err.Error(), "devUrl") {
		t.Errorf("invalid devUrl accepted: %v", err)
	}

	// A frontend at the project root.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, err := loadConfig(root); err != nil || c.Bindings != filepath.Join("src", "mygo.ts") {
		t.Errorf("bindings with package.json at the root = %q, %v", c.Bindings, err)
	}
}

func TestInfoPlist(t *testing.T) {
	c := &Config{Name: "A & B", Identifier: "com.example.ab", Version: "1.2.3", MacOS: MacOS{MinimumSystemVersion: "12.0"}, URLSchemes: []string{"ab"}}
	plist := string(infoPlist(c, "A & B", "icon.icns"))
	for _, want := range []string{
		"<string>A &amp; B</string>",
		"<key>CFBundleIdentifier</key>\n\t<string>com.example.ab</string>",
		"<key>CFBundleIconFile</key>\n\t<string>icon.icns</string>",
		"<key>CFBundleURLSchemes</key>",
		"<string>ab</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("Info.plist missing %q:\n%s", want, plist)
		}
	}
}

func TestIcons(t *testing.T) {
	src := defaultIcon()
	if err := validateIcon(src); err != nil {
		t.Fatal(err)
	}
	icns, err := pngToICNS(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(icns[:4]) != "icns" || int(binary.BigEndian.Uint32(icns[4:])) != len(icns) {
		t.Fatalf("bad icns header")
	}
	// Walk the entries and decode one of them.
	found := 0
	for off := 8; off < len(icns); {
		kind, size := string(icns[off:off+4]), int(binary.BigEndian.Uint32(icns[off+4:]))
		if kind == "ic07" {
			img, err := png.Decode(bytes.NewReader(icns[off+8 : off+size]))
			if err != nil || img.Bounds().Dx() != 128 {
				t.Errorf("ic07 entry: %v", err)
			}
		}
		off += size
		found++
	}
	if found != 10 {
		t.Errorf("icns has %d entries", found)
	}
	ico, err := pngToICO(src)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(ico[2:]) != 1 || binary.LittleEndian.Uint16(ico[4:]) != 7 {
		t.Error("bad ico header")
	}
}

func TestWriteUniversal(t *testing.T) {
	dir := t.TempDir()
	machO := func(name string, cpu uint32, size int) string {
		b := make([]byte, size)
		binary.LittleEndian.PutUint32(b, 0xfeedfacf)
		binary.LittleEndian.PutUint32(b[4:], cpu)
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	arm, amd := machO("arm", 0x0100000c, 1000), machO("amd", 0x01000007, 3000)
	out := filepath.Join(dir, "fat")
	if err := writeUniversal(out, arm, amd); err != nil {
		t.Fatal(err)
	}
	fat, _ := os.ReadFile(out)
	if binary.BigEndian.Uint32(fat) != 0xcafebabe || binary.BigEndian.Uint32(fat[4:]) != 2 {
		t.Fatal("bad fat header")
	}
	for i, wantSize := range []uint32{1000, 3000} {
		entry := fat[8+i*20:]
		off, size := binary.BigEndian.Uint32(entry[8:]), binary.BigEndian.Uint32(entry[12:])
		if size != wantSize || off%(1<<14) != 0 || binary.LittleEndian.Uint32(fat[off:]) != 0xfeedfacf {
			t.Errorf("slice %d: offset %d size %d", i, off, size)
		}
	}
	if err := writeUniversal(out, filepath.Join(dir, "missing")); err == nil {
		t.Error("expected error for missing slice")
	}
}

func TestTemplate(t *testing.T) {
	dir := t.TempDir()
	data := templateData{Name: "Demo App", Slug: "demo-app", Module: "demo-app", Identifier: "com.example.demoapp", Runtime: "^0.1.0"}
	if err := writeTemplate(dir, data); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"main.go", "mygo.json", ".gitignore", "frontend/package.json", "frontend/vite.config.ts", "frontend/index.html", "frontend/src/main.ts"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	main, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(main), `Title:           "Demo App"`) {
		t.Error("name not rendered into main.go")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "main.go", main, 0); err != nil {
		t.Errorf("main.go does not parse: %v", err)
	}
	pkg, _ := os.ReadFile(filepath.Join(dir, "frontend", "package.json"))
	if !strings.Contains(string(pkg), `"demo-app-frontend"`) || !strings.Contains(string(pkg), `"mygo-runtime": "^0.1.0"`) {
		t.Errorf("package.json: %s", pkg)
	}
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.DevURL != "http://localhost:5173" || c.DevCommand == "" || c.BuildCommand == "" || c.FrontendDist != "frontend/dist" {
		t.Errorf("template mygo.json: %+v", c)
	}
	if slugify("Hello, World!") != "hello-world" || slugify("!!!") != "app" {
		t.Error("slugify")
	}
}

func TestPackageFlags(t *testing.T) {
	c := &Config{Name: "Bob's App", Version: "1.0.0", Identifier: "com.example.bob", URLSchemes: []string{"bob", "bob-dev"}}
	got := packageFlags(c)
	want := ` -X "github.com/egoist/mygo.packageName=Bob's App" -X github.com/egoist/mygo.packageVersion=1.0.0` +
		` -X github.com/egoist/mygo.packageIdentifier=com.example.bob -X github.com/egoist/mygo.packageURLSchemes=bob,bob-dev`
	if got != want {
		t.Errorf("packageFlags =\n%s\nwant\n%s", got, want)
	}
	if q := ldflagsQuote(`say "hi"`); q != `'say "hi"'` {
		t.Errorf("ldflagsQuote = %s", q)
	}

	dir := t.TempDir()
	write := func(json string) error {
		if err := os.WriteFile(filepath.Join(dir, "mygo.json"), []byte(json), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := loadConfig(dir)
		return err
	}
	if err := write(`{"urlSchemes": ["my app"]}`); err == nil || !strings.Contains(err.Error(), "urlSchemes") {
		t.Errorf("invalid scheme: %v", err)
	}
	if err := write(`{"name": "Both ' and \""}`); err == nil || !strings.Contains(err.Error(), "quotes") {
		t.Errorf("name with both quotes: %v", err)
	}

	// The desktop entry of Linux builds opens the app's URLs.
	c = &Config{Name: "My App", URLSchemes: []string{"myapp"}}
	files, err := writeLinuxDesktop(c, dir, "my-app")
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := os.ReadFile(files[len(files)-1])
	for _, want := range []string{"Exec=my-app %U\n", "MimeType=x-scheme-handler/myapp;\n"} {
		if !strings.Contains(string(entry), want) {
			t.Errorf("desktop entry lacks %q:\n%s", want, entry)
		}
	}
}

func TestWindowsSignCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "My App.exe")
	if err := os.WriteFile(file, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Config{root: dir, Windows: Windows{SignCommand: `printf signed >> %1`}}
	if err := signWindows(c, file); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); string(b) != "MZsigned" {
		t.Errorf("the sign command got %q", b)
	}
	c.Windows.SignCommand = "false %1"
	if err := signWindows(c, file); err == nil {
		t.Error("a failing sign command succeeded")
	}
}

func TestFileAssociations(t *testing.T) {
	c := &Config{Name: "Notes", Identifier: "com.example.notes", Version: "1.0.0", URLSchemes: []string{"notes"},
		FileAssociations: []FileAssociation{
			{Ext: []string{"md", "markdown"}, Name: "Markdown Document", MimeType: "text/markdown"},
			{Ext: []string{"note"}, Name: "Note", Role: "Viewer"},
		}}
	plist := string(infoPlist(c, "Notes", ""))
	for _, want := range []string{"<key>CFBundleDocumentTypes</key>", "<string>Markdown Document</string>", "<string>markdown</string>", "<string>Viewer</string>", "<string>text/markdown</string>"} {
		if !strings.Contains(plist, want) {
			t.Errorf("Info.plist lacks %q", want)
		}
	}
	entry := linuxDesktopEntry(c, "notes", "notes")
	if !strings.Contains(entry, "Exec=notes %U\n") || !strings.Contains(entry, "MimeType=text/markdown;application/x-notes-note;x-scheme-handler/notes;\n") {
		t.Errorf("desktop entry:\n%s", entry)
	}
	xml := mimePackage(c)
	if !strings.Contains(xml, `<mime-type type="application/x-notes-note">`) || !strings.Contains(xml, `<glob pattern="*.note"/>`) || strings.Contains(xml, "text/markdown") {
		t.Errorf("MIME package:\n%s", xml)
	}
	reg, unreg := nsisAssociations(c, "Notes.exe")
	for _, want := range []string{`"Software\Classes\.md\OpenWithProgids" "com.example.notes.md"`, `"Software\Classes\com.example.notes.note\shell\open\command" "" '"$INSTDIR\Notes.exe" "%1"'`, `"Software\Classes\com.example.notes.md\DefaultIcon" "" "$INSTDIR\Notes.exe,0"`, `"Software\Classes\notes" "URL Protocol"`, "SHChangeNotify"} {
		if !strings.Contains(reg, want) {
			t.Errorf("installer registration lacks %s:\n%s", want, reg)
		}
	}
	if !strings.Contains(unreg, `DeleteRegKey HKCU "Software\Classes\com.example.notes.md"`) || !strings.Contains(unreg, `DeleteRegKey HKCU "Software\Classes\notes"`) {
		t.Errorf("installer unregistration:\n%s", unreg)
	}

	dir := t.TempDir()
	for _, bad := range []string{`[{"ext": []}]`, `[{"ext": [".md"]}]`, `[{"ext": ["md"], "role": "Owner"}]`} {
		os.WriteFile(filepath.Join(dir, "mygo.json"), []byte(`{"fileAssociations": `+bad+`}`), 0o644)
		if _, err := loadConfig(dir); err == nil {
			t.Errorf("accepted fileAssociations %s", bad)
		}
	}
}

func TestInfoPlistExtraKeys(t *testing.T) {
	c := &Config{Name: "Cam", Identifier: "com.example.cam", Version: "1.0.0", MacOS: MacOS{
		MinimumSystemVersion: "12.0",
		InfoPlist: map[string]any{
			"NSCameraUsageDescription":             "Scan <documents> & more.",
			"LSUIElement":                          true,
			"NSSupportsAutomaticGraphicsSwitching": false,
			"MyNumbers":                            []any{float64(1), 2.5},
			"MyDict":                               map[string]any{"a": "b"},
		},
	}}
	plist := infoPlist(c, "Cam", "")
	for _, want := range []string{
		"<key>NSCameraUsageDescription</key>\n\t<string>Scan &lt;documents&gt; &amp; more.</string>",
		"<key>LSUIElement</key>\n\t<true/>",
		"<key>NSSupportsAutomaticGraphicsSwitching</key>\n\t<false/>",
		"<integer>1</integer>", "<real>2.5</real>",
	} {
		if !strings.Contains(string(plist), want) {
			t.Errorf("Info.plist lacks %q:\n%s", want, plist)
		}
	}
	if _, err := exec.LookPath("plutil"); err == nil {
		path := filepath.Join(t.TempDir(), "Info.plist")
		os.WriteFile(path, plist, 0o644)
		if out, err := exec.Command("plutil", "-lint", path).CombinedOutput(); err != nil {
			t.Errorf("plutil: %v\n%s", err, out)
		}
	}
}
