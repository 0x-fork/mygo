package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMain lets the test binary play a development build of an app for
// the launch tests.
func TestMain(m *testing.M) {
	mode := os.Getenv("MYGO_FAKE_APP")
	if mode == "relaunch" {
		// mygo.App.Relaunch the first time, then like "ready".
		marker := os.Getenv("MYGO_FAKE_MARKER")
		if _, err := os.Stat(marker); err != nil {
			_ = os.WriteFile(marker, nil, 0o644)
			os.Exit(devRelaunchCode)
		}
		mode = "ready"
	}
	switch mode {
	case "":
		os.Exit(m.Run())
	case "ready":
		// Like the mygo package: report ready, run until SIGTERM.
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM)
		conn, err := net.Dial("unix", os.Getenv("MYGO_READY_SOCKET"))
		if err != nil {
			os.Exit(2)
		}
		_, _ = conn.Write([]byte("ready\n"))
		conn.Close()
		<-sig
		os.Exit(0)
	case "exit":
		os.Exit(3)

	case "hang":
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(time.Hour)
	}
}

func TestDevLaunch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix only")
	}
	defer func(d time.Duration) { stopGrace = d }(stopGrace)
	stopGrace = 5 * time.Second // exiting takes a while under the race detector
	s := &devSession{root: t.TempDir(), readyTimeout: 5 * time.Second}
	ctx := context.Background()

	t.Setenv("MYGO_FAKE_APP", "ready")
	p, err := s.launch(ctx, os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	p.stop()
	if p.err != nil {
		t.Errorf("a stopped build should quit cleanly: %v", p.err)
	}

	// A build that relaunches itself before it is ready starts again.
	t.Setenv("MYGO_FAKE_APP", "relaunch")
	t.Setenv("MYGO_FAKE_MARKER", filepath.Join(t.TempDir(), "launched"))
	p, err = s.launch(ctx, os.Args[0])
	if err != nil {
		t.Fatalf("relaunch: %v", err)
	}
	p.stop()

	t.Setenv("MYGO_FAKE_APP", "exit")
	if _, err := s.launch(ctx, os.Args[0]); err == nil || !strings.Contains(err.Error(), "exited before it was ready: exit status 3") {
		t.Errorf("early exit: %v", err)
	}

	t.Setenv("MYGO_FAKE_APP", "hang")
	s.readyTimeout = 300 * time.Millisecond
	stopGrace = 200 * time.Millisecond
	start := time.Now()
	if _, err := s.launch(ctx, os.Args[0]); err == nil || !strings.Contains(err.Error(), "did not get ready") {
		t.Errorf("hang: %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("a hanging build took %v to be killed", d)
	}
}

func TestWatcher(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := &watcher{}
	w.set(&buildInputs{sourceDirs: []string{dir}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes := w.watch(ctx, 20*time.Millisecond)
	expect := func(changed bool, what string) {
		t.Helper()
		select {
		case <-changes:
			if !changed {
				t.Fatalf("%s: change reported", what)
			}
		case <-time.After(300 * time.Millisecond):
			if changed {
				t.Fatalf("%s: change not reported", what)
			}
		}
	}
	expect(false, "nothing")
	for _, f := range []string{"main_test.go", "notes.txt", "web.ts"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	expect(false, "files the build ignores")
	if err := os.WriteFile(file, []byte("package main // edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	expect(true, "edited source")
	if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	expect(true, "new source")

	// An edit made while a build runs is not lost when the build hands
	// over the same inputs.
	if err := os.WriteFile(file, []byte("package main // edited during a build"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.set(&buildInputs{sourceDirs: []string{dir}})
	expect(true, "edit during a build")

	// Resources are watched whole, and may not exist yet.
	res := filepath.Join(dir, "resources")
	w.set(&buildInputs{sourceDirs: []string{dir}, trees: []string{res}})
	expect(false, "new inputs")
	writeFiles(t, res, map[string]string{"data/words.txt": "hello"})
	expect(true, "new resources")
	writeFiles(t, res, map[string]string{"data/.DS_Store": "junk"})
	expect(false, "hidden file in resources")
	writeFiles(t, res, map[string]string{"data/words.txt": "hello, world"})
	expect(true, "edited resource")
	if err := os.RemoveAll(res); err != nil {
		t.Fatal(err)
	}
	expect(true, "removed resources")
}

// TestListBuildInputs lists the inputs of the mygo command itself.
func TestListBuildInputs(t *testing.T) {
	root, _ := filepath.Abs("../..")
	c := &Config{root: root, Main: "./cmd/mygo", Icon: "icon.png", Resources: []string{"notes"},
		MacOS: MacOS{Entitlements: "app.plist", HelperEntitlements: map[string]string{"bin/tool": "tool.plist"}}}
	in, err := listBuildInputs(c)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(in.sourceDirs, filepath.Join(root, "cmd", "mygo")) {
		t.Errorf("sourceDirs lacks cmd/mygo: %q", in.sourceDirs)
	}
	for _, d := range in.sourceDirs {
		if strings.Contains(d, string(filepath.Separator)+"pkg"+string(filepath.Separator)+"mod"+string(filepath.Separator)) || strings.HasPrefix(d, goEnv("GOROOT")) {
			t.Errorf("sourceDirs has %s, which never changes", d)
		}
	}
	for _, f := range []string{filepath.Join(root, "go.mod"), filepath.Join(root, "mygo.json"), filepath.Join(root, "icon.png"),
		filepath.Join(root, "app.plist"), filepath.Join(root, "tool.plist")} {
		if !slices.Contains(in.files, f) {
			t.Errorf("files lacks %s: %q", f, in.files)
		}
	}
	if !slices.Equal(in.trees, []string{filepath.Join(root, "notes"), filepath.Join(root, "resources")}) {
		t.Errorf("trees = %q", in.trees)
	}
	// The CLI embeds its project template.
	if !slices.Contains(in.fileDirs, filepath.Join(root, "cmd", "mygo", "template")) {
		t.Errorf("fileDirs lacks the embedded template: %q", in.fileDirs)
	}
}

// TestHashEntitlements tells changes to the entitlements of development
// apps apart, which relaunch them.
func TestHashEntitlements(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"app.plist": "app", "tool.plist": "tool"})
	c := &Config{root: dir}
	hash := func() string {
		t.Helper()
		var b bytes.Buffer
		if err := hashEntitlements(&b, c); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	seen := map[string]string{}
	for _, step := range []struct {
		name string
		edit func()
	}{
		{"none", func() {}},
		{"app", func() { c.MacOS.Entitlements = "app.plist" }},
		{"helper", func() { c.MacOS.HelperEntitlements = map[string]string{"bin/tool": "tool.plist"} }},
		{"edited", func() { writeFiles(t, dir, map[string]string{"tool.plist": "tool, edited"}) }},
		{"renamed", func() { c.MacOS.HelperEntitlements = map[string]string{"bin/other": "tool.plist"} }},
	} {
		step.edit()
		h := hash()
		if prev, ok := seen[h]; ok {
			t.Errorf("%s: same hash as %s", step.name, prev)
		}
		seen[h] = step.name
	}
	c.MacOS.Entitlements = "missing.plist"
	if err := hashEntitlements(io.Discard, c); err == nil {
		t.Error("missing entitlements hashed")
	}
}

func TestDevConfig(t *testing.T) {
	c := &Config{Name: "Todo", Identifier: "dev.mygo.todo"}
	d := devConfig(c)
	if d.Name != "Todo Dev" || d.Identifier != "dev.mygo.todo.dev" || c.Name != "Todo" {
		t.Errorf("devConfig = %q %q (original %q)", d.Name, d.Identifier, c.Name)
	}
}
