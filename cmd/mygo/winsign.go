package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Windows configures Windows packaging.
type Windows struct {
	// Certificate is a code signing certificate (.pfx) to sign the
	// executable and the installer with, which keeps SmartScreen from
	// warning users. Its password comes from
	// MYGO_WINDOWS_CERTIFICATE_PASSWORD. signtool signs on Windows,
	// osslsigncode elsewhere.
	Certificate string `json:"certificate"`
	// SignCommand signs a file instead, with %1 standing for its path, e.g.
	// for Azure Trusted Signing or a certificate on a hardware token:
	//   "signtool sign /fd sha256 /tr http://timestamp.digicert.com /td sha256 /a %1"
	SignCommand string `json:"signCommand"`
	// TimestampURL is the RFC 3161 time stamping server of Certificate
	// (default http://timestamp.digicert.com).
	TimestampURL string `json:"timestampUrl"`
}

func (w *Windows) signs() bool { return w.Certificate != "" || w.SignCommand != "" }

// signWindows signs an executable or installer.
func signWindows(c *Config, file string) error {
	w := c.Windows
	if w.SignCommand != "" {
		// Not echoed: the command may hold credentials.
		logf("signing %s", filepath.Base(file))
		if err := shellCommand(c.root, strings.ReplaceAll(w.SignCommand, "%1", shellQuote(file))).Run(); err != nil {
			return fmt.Errorf("windows.signCommand: %w", err)
		}
		return nil
	}
	password := os.Getenv("MYGO_WINDOWS_CERTIFICATE_PASSWORD")
	cert := c.path(w.Certificate)
	ts := w.TimestampURL
	if ts == "" {
		ts = "http://timestamp.digicert.com"
	}
	logf("signing %s", filepath.Base(file))
	if runtime.GOOS == "windows" {
		tool := signtool()
		if tool == "" {
			return errors.New("signing needs signtool from the Windows SDK")
		}
		args := []string{"sign", "/f", cert, "/fd", "sha256", "/tr", ts, "/td", "sha256"}
		if password != "" {
			args = append(args, "/p", password)
		}
		if out, err := exec.Command(tool, append(args, file)...).CombinedOutput(); err != nil {
			return fmt.Errorf("signtool: %v\n%s", err, out)
		}
		return nil
	}
	tool, err := exec.LookPath("osslsigncode")
	if err != nil {
		return errors.New("signing Windows apps on this system needs osslsigncode (brew install osslsigncode, apt install osslsigncode)")
	}
	signed := file + ".signed"
	args := []string{"sign", "-pkcs12", cert, "-h", "sha256", "-ts", ts, "-in", file, "-out", signed}
	if password != "" {
		args = append(args, "-pass", password)
	}
	if out, err := exec.Command(tool, args...).CombinedOutput(); err != nil {
		os.Remove(signed)
		return fmt.Errorf("osslsigncode: %v\n%s", err, out)
	}
	return os.Rename(signed, file)
}

// signtool finds signtool.exe of the newest Windows SDK.
func signtool() string {
	if p, err := exec.LookPath("signtool"); err == nil {
		return p
	}
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	matches, _ := filepath.Glob(filepath.Join(os.Getenv("ProgramFiles(x86)"), "Windows Kits", "10", "bin", "10.*", arch, "signtool.exe"))
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	return matches[len(matches)-1]
}

// shellQuote quotes an argument for the shell that run uses.
func shellQuote(s string) string {
	if runtime.GOOS == "windows" {
		return `"` + s + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
