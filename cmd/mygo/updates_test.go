package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
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

func TestUpdatesConfig(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	key := base64.StdEncoding.EncodeToString(pub)
	dir := t.TempDir()
	load := func(updates string) (*Config, error) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "mygo.json"), []byte(`{"name": "My App", "version": "1.2.0", "updates": `+updates+`}`), 0o644); err != nil {
			t.Fatal(err)
		}
		return loadConfig(dir)
	}
	c, err := load(`{"publicKey": "` + key + `", "github": "me/my-app"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.updateFeed("darwin-arm64"); got != "https://github.com/me/my-app/releases/latest/download/update-darwin-arm64.json" {
		t.Errorf("feed = %s", got)
	}
	if got := c.updateFile("my-app-1.2.0-darwin-arm64.tar.gz", false); got != "https://github.com/me/my-app/releases/download/v1.2.0/my-app-1.2.0-darwin-arm64.tar.gz" {
		t.Errorf("archive = %s", got)
	}
	c, err = load(`{"publicKey": "` + key + `", "url": "https://dl.example.com/my-app/"}`)
	if err != nil || c.updateFeed("linux-amd64") != "https://dl.example.com/my-app/update-linux-amd64.json" {
		t.Errorf("url feed = %s, %v", c.updateFeed("linux-amd64"), err)
	}
	for _, bad := range []string{
		`{"github": "me/my-app"}`,
		`{"publicKey": "nope", "github": "me/my-app"}`,
		`{"publicKey": "` + key + `"}`,
		`{"publicKey": "` + key + `", "github": "me"}`,
		`{"publicKey": "` + key + `", "url": "http://dl.example.com"}`,
	} {
		if _, err := load(bad); err == nil {
			t.Errorf("accepted updates %s", bad)
		}
	}
}

func TestKeygen(t *testing.T) {
	dir := t.TempDir()
	if err := runKeygen([]string{"-o", dir}); err != nil {
		t.Fatal(err)
	}
	priv, _ := os.ReadFile(filepath.Join(dir, "mygo-update.key"))
	pub, _ := os.ReadFile(filepath.Join(dir, "mygo-update.pub"))
	sk, err := update.ParsePrivateKey(string(priv))
	if err != nil {
		t.Fatal(err)
	}
	if base64.StdEncoding.EncodeToString(sk.Public().(ed25519.PublicKey)) != strings.TrimSpace(string(pub)) {
		t.Error("the keys are not a pair")
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(filepath.Join(dir, "mygo-update.key")); info.Mode().Perm() != 0o600 {
			t.Errorf("secret key mode %v", info.Mode())
		}
	}
	if err := runKeygen([]string{"-o", dir}); err == nil {
		t.Error("keygen replaced existing keys without -force")
	}
}

// TestUpdateEndToEnd builds two versions of an app, installs the first and
// lets it update itself to the second from a local server.
func TestUpdateEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles programs")
	}
	dir := testModule(t, map[string]string{
		"main.go": `package main

import (
	"context"
	"fmt"
	"os"

	"github.com/egoist/mygo"
)

func main() {
	fmt.Println("version", mygo.App.Version())
	if os.Getenv("UPDATE") == "" {
		return
	}
	up, err := mygo.Updater.Check(context.Background())
	if err != nil || up == nil {
		fmt.Println("check:", up, err)
		os.Exit(1)
	}
	if err := up.Install(context.Background(), nil); err != nil {
		fmt.Println("install:", err)
		os.Exit(1)
	}
	fmt.Println("installed", up.Version, up.Notes)
}
`,
		"CHANGELOG.md": "# Changelog\n\n## 1.1.0\n\n- New things\n\n## 1.0.0\n\n- First\n",
	})
	pub, priv, _ := ed25519.GenerateKey(nil)
	t.Setenv("MYGO_UPDATER_PRIVATE_KEY", base64.StdEncoding.EncodeToString(priv))
	serve := t.TempDir()
	srv := httptest.NewServer(http.FileServer(http.Dir(serve)))
	defer srv.Close()

	goos, goarch := runtime.GOOS, runtime.GOARCH
	target := goos + "-" + goarch
	build := func(version, out string) string {
		t.Helper()
		c, err := loadConfig(dir)
		if err != nil {
			t.Fatal(err)
		}
		c.Name, c.Version, c.Out = "Update Test", version, out
		c.Updates = &Updates{PublicKey: base64.StdEncoding.EncodeToString(pub), URL: srv.URL, TagPrefix: "v"}
		opts := buildOptions{sign: "-", skipDMG: true, work: t.TempDir()}
		if opts.pkg, err = packageDir(c); err != nil {
			t.Fatal(err)
		}
		if _, err := buildPlatform(c, goos, goarch, opts); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(dir, out, target)
	}
	v1, v2 := build("1.0.0", "dist1"), build("1.1.0", "dist2")

	// Publish 1.1.0.
	data, err := os.ReadFile(filepath.Join(v2, update.ManifestName(target)))
	if err != nil {
		t.Fatal(err)
	}
	var m update.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Version != "1.1.0" || m.Notes != "- New things" {
		t.Errorf("manifest = %+v", m)
	}
	for _, name := range []string{update.ManifestName(target), filepath.Base(m.URL)} {
		b, err := os.ReadFile(filepath.Join(v2, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(serve, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Install 1.0.0 as a user would, and run it.
	install := t.TempDir()
	exe := filepath.Join(install, "update-test")
	switch goos {
	case "darwin":
		if err := exec.Command("ditto", filepath.Join(v1, "Update Test.app"), filepath.Join(install, "Update Test.app")).Run(); err != nil {
			t.Fatal(err)
		}
		exe = filepath.Join(install, "Update Test.app", "Contents", "MacOS", "Update Test")
	case "windows":
		exe = filepath.Join(install, "Update Test.exe")
		fallthrough
	default:
		entries, _ := os.ReadDir(v1)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".tar.gz") || strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			if err := copyResource(filepath.Join(v1, e.Name()), filepath.Join(install, e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	run := func(env ...string) string {
		t.Helper()
		cmd := exec.Command(exe)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", exe, err, out)
		}
		return string(out)
	}
	if out := run("UPDATE=1"); !strings.Contains(out, "version 1.0.0") || !strings.Contains(out, "installed 1.1.0 - New things") {
		t.Fatalf("updating printed:\n%s", out)
	}
	if out := run(); !strings.Contains(out, "version 1.1.0") {
		t.Errorf("after the update the app printed:\n%s", out)
	}
	if goos == "darwin" {
		if out, err := exec.Command("codesign", "--verify", "--deep", "--strict", filepath.Join(install, "Update Test.app")).CombinedOutput(); err != nil {
			t.Errorf("the updated bundle fails codesign: %v\n%s", err, out)
		}
	}
}

func TestPublishGitHub(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as gh")
	}
	bin, dist := t.TempDir(), t.TempDir()
	calls := filepath.Join(bin, "calls")
	// gh: the release does not exist yet; every other call succeeds.
	script := "#!/bin/sh\necho \"$@\" >> " + calls + "\n[ \"$2\" = view ] && exit 1\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var artifacts []string
	for _, name := range []string{"App 1.2.0.dmg", "app-1.2.0-darwin-arm64.tar.gz", "update-darwin-arm64.json", "App Setup 1.2.0.exe", "App.exe"} {
		p := filepath.Join(dist, name)
		os.WriteFile(p, nil, 0o644)
		artifacts = append(artifacts, p)
	}
	os.Mkdir(filepath.Join(dist, "App.app"), 0o755)
	artifacts = append(artifacts, filepath.Join(dist, "App.app"))
	c := &Config{root: t.TempDir(), Version: "1.2.0", Updates: &Updates{GitHub: "me/app", TagPrefix: "v"}}
	if err := publishGitHub(c, artifacts); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(calls)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[1], "release create v1.2.0 --repo me/app --draft") {
		t.Fatalf("gh calls:\n%s", b)
	}
	if !strings.Contains(lines[2], "App 1.2.0.dmg") || !strings.Contains(lines[2], "App Setup 1.2.0.exe") || strings.Contains(lines[2], "App.exe ") || strings.Contains(lines[2], "update-") {
		t.Errorf("first upload: %s", lines[2])
	}
	if !strings.HasSuffix(lines[3], "update-darwin-arm64.json") {
		t.Errorf("the manifests must go last: %s", lines[3])
	}
}
