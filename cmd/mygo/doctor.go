package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func runDoctor(args []string) error {
	flags := newFlags("doctor", "", "Checks the tools and system libraries MyGo needs.")
	if err := flags.Parse(args); err != nil {
		return err
	}
	ok := true
	check := func(name string, good bool, detail string) {
		mark := "\033[32m✓\033[0m"
		if !good {
			mark, ok = "\033[31m✗\033[0m", false
		}
		fmt.Printf("%s %-10s %s\n", mark, name, detail)
	}

	goOut, err := exec.Command("go", "version").Output()
	check("go", err == nil, strings.TrimSpace(string(goOut)))
	if bunOut, err := exec.Command("bun", "--version").Output(); err == nil {
		check("bun", true, strings.TrimSpace(string(bunOut)))
	} else {
		fmt.Println("- bun        not found (optional, used by the frontend template: https://bun.sh)")
	}

	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("sw_vers", "-productVersion").Output()
		v := strings.TrimSpace(string(out))
		major, _ := strconv.Atoi(strings.Split(v, ".")[0])
		check("macOS", err == nil && major >= 12, v+" (WKWebView; macOS 12 or later is required)")
	case "linux":
		out, _ := exec.Command("sh", "-c", "ldconfig -p 2>/dev/null | grep -o 'libwebkit2gtk-4\\.[01]\\.so[.0-9]*' | sort -u").Output()
		libs := strings.TrimSpace(string(out))
		check("webkit", libs != "", or(libs, "WebKitGTK not found: install libwebkit2gtk-4.1-0 (Debian/Ubuntu) or webkit2gtk4.1 (Fedora)"))
	case "windows":
		out, err := exec.Command("reg", "query", `HKLM\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`, "/v", "pv").Output()
		if err != nil {
			out, err = exec.Command("reg", "query", `HKCU\Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`, "/v", "pv").Output()
		}
		check("webview2", err == nil, or(lastField(string(out)), "WebView2 runtime not found: https://go.microsoft.com/fwlink/p/?LinkId=2124703"))
	default:
		check(runtime.GOOS, false, "no MyGo backend for this platform yet")
	}
	if !ok {
		return fmt.Errorf("some checks failed")
	}
	return nil
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func lastField(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}
