package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/update"
)

func TestInstallScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runs install.sh")
	}
	if os.Geteuid() == 0 {
		t.Skip("install.sh refuses to install for root")
	}
	// uname answers as an x86-64 Linux machine would.
	bin := t.TempDir()
	uname := "#!/bin/sh\ncase \"$1\" in -s) echo Linux ;; -m) echo x86_64 ;; esac\n"
	if err := os.WriteFile(filepath.Join(bin, "uname"), []byte(uname), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	home := filepath.Join(t.TempDir(), "Jane Doe")
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mygo.json"), []byte(`{
		"name": "My App",
		"icon": "icon.png",
		"urlSchemes": ["my-app"],
		"fileAssociations": [{"ext": ["note"], "name": "Note"}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "icon.png"), []byte("png"), 0o644)
	// build stages a fake app of version and writes its archive and
	// install.sh, returning where they are.
	build := func(c *Config, version string) string {
		t.Helper()
		c.Version = version
		stage := filepath.Join(t.TempDir(), "linux-amd64")
		files := map[string]string{
			"my-app":                "#!/bin/sh\necho " + version + "\n",
			"only-" + version:       "",
			"resources/config.json": "{}",
		}
		for name, content := range files {
			os.MkdirAll(filepath.Join(stage, filepath.Dir(name)), 0o755)
			if err := os.WriteFile(filepath.Join(stage, name), []byte(content), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := writeLinuxDesktop(c, stage, "my-app"); err != nil {
			t.Fatal(err)
		}
		entries, _ := os.ReadDir(stage)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		if _, err := writeArchive(c, stage, "linux-amd64", names); err != nil {
			t.Fatal(err)
		}
		if _, err := writeInstallScript(c, stage); err != nil {
			t.Fatal(err)
		}
		return stage
	}
	run := func(script string, args ...string) string {
		t.Helper()
		out, err := exec.Command("sh", append([]string{script}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("install.sh %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	appDir := filepath.Join(home, ".local", "my-app.app")
	installed := func(version string) {
		t.Helper()
		if out, err := exec.Command(filepath.Join(home, ".local", "bin", "my-app")).Output(); err != nil || string(out) != version+"\n" {
			t.Errorf("my-app printed %q, %v, want %s", out, err, version)
		}
		if _, err := os.Stat(filepath.Join(appDir, "resources", "config.json")); err != nil {
			t.Error(err)
		}
		entries, _ := os.ReadDir(appDir)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "only-") && e.Name() != "only-"+version {
				t.Errorf("%s of another version stayed", e.Name())
			}
		}
		entry, _ := os.ReadFile(filepath.Join(home, ".local", "share", "applications", "my-app.desktop"))
		for _, line := range []string{"Name=My App", `Exec="` + appDir + `/my-app" %U`, "Icon=" + appDir + "/my-app.png", "MimeType=application/x-my-app-note;x-scheme-handler/my-app;"} {
			if !strings.Contains(string(entry), line+"\n") {
				t.Errorf("the desktop entry has no %s:\n%s", line, entry)
			}
		}
		if mime, _ := os.ReadFile(filepath.Join(home, ".local", "share", "mime", "packages", "my-app.xml")); !strings.Contains(string(mime), `<glob pattern="*.note"/>`) {
			t.Errorf("MIME package:\n%s", mime)
		}
		// Updates register the entry again as install.sh does.
		os.WriteFile(filepath.Join(home, ".local", "share", "applications", "my-app.desktop"), append(entry, "X-Stale=true\n"...), 0o644)
		if err := update.RefreshDesktopEntry(appDir, "my-app"); err != nil {
			t.Fatal(err)
		}
		if again, _ := os.ReadFile(filepath.Join(home, ".local", "share", "applications", "my-app.desktop")); string(again) != string(entry) {
			t.Errorf("updates register the entry as\n%s\ninstall.sh as\n%s", again, entry)
		}
	}

	// Without updates, the archive next to the script.
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	local := build(c, "1.0.0")
	script, _ := os.ReadFile(filepath.Join(local, "install.sh"))
	if strings.Contains(string(script), "curl -fsSL http") {
		t.Errorf("install.sh without updates tells to download it:\n%s", script)
	}
	if out := run(filepath.Join(local, "install.sh")); !strings.Contains(out, "run "+filepath.Join(home, ".local", "bin", "my-app")) {
		t.Errorf("install.sh printed:\n%s", out)
	}
	installed("1.0.0")

	// With updates, the latest version, which the manifest names.
	if _, err := exec.LookPath("curl"); err != nil {
		if _, err := exec.LookPath("wget"); err != nil {
			t.Skip("downloading needs curl or wget")
		}
	}
	pub, priv, _ := ed25519.GenerateKey(nil)
	t.Setenv("MYGO_UPDATER_PRIVATE_KEY", base64.StdEncoding.EncodeToString(priv))
	served := t.TempDir()
	srv := httptest.NewServer(http.FileServer(http.Dir(served)))
	defer srv.Close()
	none := 0
	c.Updates = &Updates{PublicKey: base64.StdEncoding.EncodeToString(pub), URL: srv.URL, Deltas: &none}
	published := build(c, "1.1.0")
	for _, name := range []string{"my-app-1.1.0-linux-amd64.tar.gz", "update-linux-amd64.json"} {
		b, err := os.ReadFile(filepath.Join(published, name))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(served, name), b, 0o644)
	}
	script, _ = os.ReadFile(filepath.Join(published, "install.sh"))
	if !strings.Contains(string(script), "curl -fsSL "+srv.URL+"/install.sh | sh") {
		t.Errorf("install.sh does not tell how to run it:\n%s", script)
	}
	alone := filepath.Join(t.TempDir(), "install.sh")
	os.WriteFile(alone, script, 0o755)
	if out := run(alone); !strings.Contains(out, "Downloading My App 1.1.0") {
		t.Errorf("install.sh printed:\n%s", out)
	}
	installed("1.1.0")

	// The archive given, over the installed version.
	run(alone, filepath.Join(local, "my-app-1.0.0-linux-amd64.tar.gz"))
	installed("1.0.0")

	// Uninstalling removes what install.sh and the app made, and only that.
	handler := filepath.Join(home, ".local", "share", "applications", "com.mygo.my-app.url-handler.desktop")
	other := filepath.Join(home, ".local", "share", "applications", "other.url-handler.desktop")
	os.WriteFile(handler, []byte(`Exec="`+appDir+`/my-app" %u`+"\n"), 0o644)
	os.WriteFile(other, []byte("Exec=/usr/bin/other %u\n"), 0o644)
	run(alone, "--uninstall")
	for _, p := range []string{appDir, filepath.Join(home, ".local", "bin", "my-app"), handler,
		filepath.Join(home, ".local", "share", "applications", "my-app.desktop"),
		filepath.Join(home, ".local", "share", "mime", "packages", "my-app.xml")} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("%s is still there", p)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("the entry of another app was removed: %v", err)
	}
	if out, err := exec.Command("sh", alone, "--uninstall").CombinedOutput(); err == nil {
		t.Errorf("uninstalling twice succeeded:\n%s", out)
	}
}
