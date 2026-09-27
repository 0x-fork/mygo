package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestWindowsInstaller builds the installer of an app where NSIS is
// installed and, on Windows, installs and uninstalls it silently.
func TestWindowsInstaller(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles programs")
	}
	if makensis() == "" {
		t.Skip("NSIS is not installed")
	}
	dir := testModule(t, map[string]string{
		"main.go":              "package main\n\nfunc main() {}\n",
		"mygo.json":            `{"name": "Setup Test", "identifier": "com.example.setuptest", "version": "2.0.0"}`,
		"resources/data/a.txt": "a",
		"resources/icon.png":   string(defaultIcon()),
	})
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	opts := buildOptions{sign: "-", work: t.TempDir()}
	if opts.pkg, err = packageDir(c); err != nil {
		t.Fatal(err)
	}
	artifacts, err := buildPlatform(c, "windows", "amd64", opts)
	if err != nil {
		t.Fatal(err)
	}
	setup := artifacts[len(artifacts)-1]
	if filepath.Base(setup) != "Setup Test Setup 2.0.0.exe" {
		t.Fatalf("artifacts = %q", artifacts)
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return
	}
	install := filepath.Join(t.TempDir(), "Setup Test")
	if out, err := exec.Command(setup, "/S", "/D="+install).CombinedOutput(); err != nil {
		t.Fatalf("installing: %v\n%s", err, out)
	}
	for _, f := range []string{"Setup Test.exe", "data/a.txt", "Uninstall.exe"} {
		if _, err := os.Stat(filepath.Join(install, filepath.FromSlash(f))); err != nil {
			t.Errorf("not installed: %s", f)
		}
	}
	if out, err := exec.Command(filepath.Join(install, "Uninstall.exe"), "/S").CombinedOutput(); err != nil {
		t.Fatalf("uninstalling: %v\n%s", err, out)
	}
	// The uninstaller copies itself away and runs from there.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(install); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the uninstaller left the app installed")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
