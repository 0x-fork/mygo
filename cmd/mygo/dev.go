package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

func runDev(args []string) error {
	flags := newFlags("dev", "[flags] [dir]", `Develops the app with live reload. It runs devCommand from mygo.json (such
as a Vite dev server), waits for devUrl to answer, then builds a development
app, which loads devUrl in place of its built frontend, and launches it.
Without devUrl, the app serves frontendDist from disk. On macOS the
development app is a real bundle, "<name> Dev" with the identifier
"<identifier>.dev", in .mygo/dev.

Changes to the Go code, mygo.json, the icon or the resources rebuild the
app, regenerate the TypeScript client and relaunch it. The new build
replaces the running one once it has started, so a build that fails or
crashes keeps the previous one running. Frontend changes are left to the
dev server. Quitting the app ends mygo dev.`)
	skipDevCommand := flags.Bool("skip-dev-command", false, "do not run devCommand")
	sign := flags.String("sign", "-", "macOS signing identity for the development app")
	if err := flags.Parse(args); err != nil {
		return err
	}
	c, err := loadConfig(dirArg(flags.Args()))
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s := &devSession{root: c.root, sign: *sign, env: []string{"MYGO_ENV=development"}}
	var exited chan error // the dev command's
	if c.DevCommand != "" && !*skipDevCommand {
		logf("starting %s", c.DevCommand)
		cmd := shellCommand(c.root, c.DevCommand)
		// Stopping the command makes script runners complain; that is noise.
		out, errOut := &gate{w: os.Stdout}, &gate{w: os.Stderr}
		cmd.Stdout, cmd.Stderr = out, errOut
		if isTerminal(os.Stdout) && os.Getenv("NO_COLOR") == "" {
			cmd.Env = append(cmd.Env, "FORCE_COLOR=1") // it now writes to a pipe
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		defer func() {
			out.close()
			errOut.close()
			terminate(cmd)
		}()
		exited = make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
	}
	switch {
	case c.DevURL != "":
		if exited == nil {
			logf("waiting for %s", c.DevURL)
		}
		wait, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := waitForURL(wait, c.DevURL, exited)
		cancel()
		if err != nil {
			return err
		}
		s.env = append(s.env, "MYGO_DEV_URL="+c.DevURL)
	case c.FrontendDist != "":
		s.env = append(s.env, "MYGO_FRONTEND_DIST="+c.path(c.FrontendDist))
	}
	return s.run(ctx, c)
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// gate writes to w until it is closed.
type gate struct {
	mu     sync.Mutex
	w      io.Writer
	closed bool
}

func (g *gate) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.closed {
		_, _ = g.w.Write(p)
	}
	return len(p), nil
}

func (g *gate) close() {
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()
}

// devSession builds and runs development builds of an app.
type devSession struct {
	root string
	sign string
	env  []string // for the app

	readyTimeout time.Duration // how long a launch may take (default 20s)

	// Used by one build at a time.
	iconKey  string
	icns     []byte
	launches int

	// Used by the run loop only.
	app *devProcess // the running build
	sum [32]byte    // fingerprint of the running build
}

var errUnchanged = errors.New("unchanged")

// run launches the app and rebuilds it on changes until the app quits or
// ctx is done.
func (s *devSession) run(ctx context.Context, c *Config) error {
	w := &watcher{}
	if in, err := listBuildInputs(c); err == nil {
		w.set(in)
	} else {
		logf("%v", err)
	}
	changes := w.watch(ctx, 250*time.Millisecond)

	type result struct {
		p      *devProcess
		sum    [32]byte
		err    error
		inputs *buildInputs
	}
	results := make(chan result, 1)
	builds, cancel := context.WithCancel(ctx)
	building, pending := false, false
	start := func() {
		building, pending = true, false
		running := s.sum
		go func() {
			p, sum, err := s.buildAndLaunch(builds, running)
			// What the build reads may have changed, e.g. a new import.
			var inputs *buildInputs
			if c, cerr := loadConfig(s.root); cerr == nil {
				inputs, _ = listBuildInputs(c)
			}
			results <- result{p, sum, err, inputs}
		}()
	}
	var stopping sync.WaitGroup
	defer func() {
		cancel()
		if building {
			if r := <-results; r.p != nil {
				r.p.stop()
			}
		}
		if s.app != nil {
			s.app.stop()
		}
		stopping.Wait()
	}()

	start()
	for {
		var exited <-chan struct{}
		if s.app != nil {
			exited = s.app.done
		}
		select {
		case <-ctx.Done():
			return nil
		case <-exited:
			err := s.app.err
			s.app, s.sum = nil, [32]byte{}
			if err == nil {
				logf("the app quit")
				return nil
			}
			logf("the app exited (%v); waiting for changes", err)
		case <-changes:
			if building {
				pending = true
			} else {
				start()
			}
		case r := <-results:
			building = false
			if r.inputs != nil {
				w.set(r.inputs)
			}
			switch {
			case errors.Is(r.err, errUnchanged):
				logf("the build did not change")
			case r.err != nil:
				logf("%v", r.err)
				if s.app != nil {
					logf("keeping the previous build running")
				}
			default:
				prev := s.app
				s.app, s.sum = r.p, r.sum
				if prev != nil {
					stopping.Add(1)
					go func() {
						defer stopping.Done()
						prev.stop()
					}()
				}
			}
			if pending {
				start()
			}
		}
	}
}

// devConfig configures the development app: "<Name> Dev" with the
// identifier "<identifier>.dev", so that its data, preferences and single
// instance lock stay apart from the production app's.
func devConfig(c *Config) *Config {
	d := *c
	d.Name += " Dev"
	d.Identifier += ".dev"
	return &d
}

// buildAndLaunch builds the app and launches it once it differs from the
// running build, whose fingerprint is running. It returns the new process
// after it reported ready.
func (s *devSession) buildAndLaunch(ctx context.Context, running [32]byte) (*devProcess, [32]byte, error) {
	var sum [32]byte
	c, err := loadConfig(s.root)
	if err != nil {
		return nil, sum, err
	}
	started := time.Now()
	dir := filepath.Join(s.root, ".mygo", "dev", runtime.GOOS+"-"+runtime.GOARCH)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, sum, err
	}
	stage, err := os.MkdirTemp(dir, ".staging-")
	if err != nil {
		return nil, sum, err
	}
	defer os.RemoveAll(stage)

	dc := devConfig(c)
	name := dc.executableName()
	switch runtime.GOOS {
	case "darwin":
	case "windows":
		name += ".exe"
	default:
		name = slugify(dc.Name)
	}
	bin := filepath.Join(stage, name)
	if running == ([32]byte{}) {
		logf("building %s", dc.Name)
	} else {
		logf("rebuilding")
	}
	if err := buildBinaryContext(ctx, c, bin, nil, "-ldflags", strings.TrimSpace(packageFlags(dc))); err != nil {
		return nil, sum, err
	}

	res, err := dc.resources(reservedNames(dc, runtime.GOOS)...)
	if err != nil {
		return nil, sum, err
	}

	// Fingerprint what the app is made of, to skip relaunching when a
	// change did not affect it.
	h := sha256.New()
	f, err := os.Open(bin)
	if err != nil {
		return nil, sum, err
	}
	_, err = io.Copy(h, f)
	f.Close()
	if err != nil {
		return nil, sum, err
	}
	if err := hashResources(h, res); err != nil {
		return nil, sum, err
	}
	var icns []byte
	if runtime.GOOS == "darwin" {
		if icns, err = s.icon(c); err != nil {
			return nil, sum, err
		}
		iconFile := ""
		if icns != nil {
			iconFile = bundleIcon
		}
		h.Write(icns)
		h.Write(infoPlist(dc, name, iconFile))
	}
	copy(sum[:], h.Sum(nil))
	if sum == running {
		return nil, sum, errUnchanged
	}

	if err := generateBindings(c, bin); err != nil {
		return nil, sum, err
	}
	exe := filepath.Join(dir, name)
	if runtime.GOOS == "darwin" {
		app, err := writeBundle(dc, stage, bin, icns, res)
		if err != nil {
			return nil, sum, err
		}
		if err := codesign(dc, app, s.sign, false); err != nil {
			return nil, sum, err
		}
		final := filepath.Join(dir, filepath.Base(app))
		if err := replacePath(app, final); err != nil {
			return nil, sum, err
		}
		exe = bundleExecutable(final)
	} else if err := placeBuild(stage, dir, res); err != nil {
		return nil, sum, err
	}
	built := time.Since(started)

	p, err := s.launch(ctx, exe)
	if err != nil {
		return nil, sum, err
	}
	verb := "started"
	if running != ([32]byte{}) {
		verb = "reloaded"
	}
	logf("%s (built in %.1fs, ready in %.1fs)", verb, built.Seconds(), (time.Since(started) - built).Seconds())
	return p, sum, nil
}

// placeBuild moves the executable in stage and the resources res next to
// it into dir, and removes what an earlier build left there and this one
// does not have.
func placeBuild(stage, dir string, res []resource) error {
	if err := copyResources(res, stage); err != nil {
		return err
	}
	entries, err := os.ReadDir(stage)
	if err != nil {
		return err
	}
	placed := map[string]bool{}
	for _, e := range entries {
		if err := replacePath(filepath.Join(stage, e.Name()), filepath.Join(dir, e.Name())); err != nil {
			return err
		}
		placed[e.Name()] = true
	}
	entries, err = os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !placed[e.Name()] && !hiddenName(e.Name()) {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}

// icon returns the app icon as .icns, rendering it only when it changed.
func (s *devSession) icon(c *Config) ([]byte, error) {
	if c.Icon == "" {
		return nil, nil
	}
	info, err := os.Stat(c.path(c.Icon))
	if err != nil {
		return nil, err
	}
	key := fmt.Sprint(c.path(c.Icon), info.Size(), info.ModTime().UnixNano())
	if key != s.iconKey {
		icns, err := appIcon(c)
		if err != nil {
			return nil, err
		}
		s.iconKey, s.icns = key, icns
	}
	return s.icns, nil
}

// launch starts a build and waits until it reports ready: the app connects
// to the Unix socket passed in MYGO_READY_SOCKET once its first window is
// ready to show.
func (s *devSession) launch(ctx context.Context, exe string) (*devProcess, error) {
	s.launches++
	sock := readySocketPath(s.launches)
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, err
	}
	defer ln.Close() // also removes the socket file
	ready := make(chan struct{})
	go func() {
		if conn, err := ln.Accept(); err == nil {
			conn.Close()
			close(ready)
		}
	}()

	cmd := exec.Command(exe)
	cmd.Dir = s.root
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.Env = append(append(os.Environ(), s.env...), "MYGO_READY_SOCKET="+sock)
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &devProcess{cmd: cmd, done: make(chan struct{})}
	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()

	wait := s.readyTimeout
	if wait == 0 {
		wait = 20 * time.Second
	}
	timeout := time.NewTimer(wait)
	defer timeout.Stop()
	select {
	case <-ready:
		return p, nil
	case <-p.done:
		if p.err == nil {
			return nil, errors.New("the app exited before it was ready")
		}
		return nil, fmt.Errorf("the app exited before it was ready: %v", p.err)
	case <-timeout.C:
		p.stop()
		return nil, fmt.Errorf("the app did not get ready within %v", wait)
	case <-ctx.Done():
		p.stop()
		return nil, ctx.Err()
	}
}

// readySocketPath returns a socket path short enough for Unix sockets
// (about 100 bytes).
func readySocketPath(n int) string {
	name := fmt.Sprintf("mygo-ready-%d-%d.sock", os.Getpid(), n)
	dir := os.TempDir()
	if len(dir)+len(name) >= 100 {
		dir = "/tmp"
	}
	return filepath.Join(dir, name)
}

// devProcess is a running development build.
type devProcess struct {
	cmd  *exec.Cmd
	done chan struct{} // closed when the process exited
	err  error         // how it exited, set before done is closed
}

// stopGrace is how long a stopped build may take to quit.
var stopGrace = 3 * time.Second

// stop asks the app to quit, like the Quit menu item, and kills it when it
// has not exited after stopGrace.
func (p *devProcess) stop() {
	select {
	case <-p.done:
		return
	default:
	}
	terminate(p.cmd)
	select {
	case <-p.done:
	case <-time.After(stopGrace):
		kill(p.cmd)
		<-p.done
	}
}
