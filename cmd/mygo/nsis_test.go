package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
		"mygo.json":            `{"name": "Setup Test", "identifier": "com.example.setuptest", "version": "2.0.0", "urlSchemes": ["setuptest"], "fileAssociations": [{"ext": ["setuptest"], "name": "Setup Test File"}]}`,
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
	// NSIS takes /D= unquoted, and Go quotes arguments with spaces.
	install := filepath.Join(t.TempDir(), "SetupTest")
	if out, err := exec.Command(setup, "/S", "/D="+install).CombinedOutput(); err != nil {
		t.Fatalf("installing: %v\n%s", err, out)
	}
	for _, f := range []string{"Setup Test.exe", "data/a.txt", "Uninstall.exe"} {
		if _, err := os.Stat(filepath.Join(install, filepath.FromSlash(f))); err != nil {
			t.Errorf("not installed: %s", f)
		}
	}
	registered := func(key string) bool {
		return exec.Command("reg", "query", `HKCU\Software\Classes\`+key).Run() == nil
	}
	for _, key := range []string{`.setuptest\OpenWithProgids`, `com.example.setuptest.setuptest\shell\open\command`, `setuptest\shell\open\command`} {
		if !registered(key) {
			t.Errorf("the installer did not register %s", key)
		}
	}
	defer func() {
		for _, key := range []string{`com.example.setuptest.setuptest`, `setuptest`} {
			if registered(key) {
				t.Errorf("the uninstaller left %s", key)
			}
		}
	}()
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

// TestWindowsSigning signs a build with a throwaway self-signed
// certificate. It runs on CI only (MYGO_TEST_SIGN), as it adds the
// certificate to the user's store for a moment.
func TestWindowsSigning(t *testing.T) {
	if runtime.GOOS != "windows" || os.Getenv("MYGO_TEST_SIGN") == "" {
		t.Skip("set MYGO_TEST_SIGN on a disposable Windows machine")
	}
	pfx := filepath.Join(t.TempDir(), "test.pfx")
	ps := func(script string) string {
		t.Helper()
		out, err := exec.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput()
		if err != nil {
			t.Fatalf("powershell: %v\n%s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	ps(`$c = New-SelfSignedCertificate -Type CodeSigningCert -Subject "CN=MyGo Test" -CertStoreLocation Cert:\CurrentUser\My; ` +
		`$p = ConvertTo-SecureString -String "secret" -Force -AsPlainText; ` +
		`Export-PfxCertificate -Cert $c -FilePath "` + pfx + `" -Password $p | Out-Null; Remove-Item $c.PSPath`)
	t.Setenv("MYGO_WINDOWS_CERTIFICATE_PASSWORD", "secret")
	dir := testModule(t, map[string]string{
		"main.go":   "package main\n\nfunc main() {}\n",
		"mygo.json": `{"name": "Signed App", "version": "1.0.0", "windows": {"certificate": "` + filepath.ToSlash(pfx) + `"}}`,
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
	for _, a := range artifacts {
		if strings.HasSuffix(a, ".exe") {
			if subject := ps(`(Get-AuthenticodeSignature "` + a + `").SignerCertificate.Subject`); subject != "CN=MyGo Test" {
				t.Errorf("%s is signed by %q", filepath.Base(a), subject)
			}
		}
	}
}
