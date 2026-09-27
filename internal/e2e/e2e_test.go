// Package e2e drives the real native backend. It needs a desktop session,
// so it only runs with MYGO_E2E=1:
//
//	MYGO_E2E=1 go test ./internal/e2e
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

type Greeter struct{}

func (Greeter) Greet(name string) string { return "Hello, " + name + "!" }

func (Greeter) Divide(a, b float64) (float64, error) {
	if b == 0 {
		return 0, errors.New("division by zero")
	}
	return a / b, nil
}

func (Greeter) WindowID(ctx context.Context) int { return mygo.CallerWindow(ctx).ID() }

// Probe counts calls, to tell whether a forged call ran.
type Probe struct{ n atomic.Int32 }

func (p *Probe) Touch() { p.n.Add(1) }

var probe = &Probe{}

type Tick struct {
	N int `json:"n"`
}

var ticked = mygo.NewEvent[Tick]("ticked")

const page = `<!doctype html><html><head><title>E2E</title></head>
<body style="margin:0;background:#1e90ff"><h1 id="h">MyGo</h1>
<script>
window.results = [];
mygo.on('ticked', (t) => results.push('tick' + t.n));
window.run = async () => {
  results.push(await mygo.call('Greeter.Greet', 'e2e'));
  results.push(await mygo.call('Greeter.Divide', 10, 4));
  try { await mygo.call('Greeter.Divide', 1, 0) } catch (e) { results.push(e.name + ':' + e.message) }
  results.push(await mygo.call('Greeter.WindowID') === mygo.windowId);
  const r = await fetch('/echo', { method: 'POST', body: 'ping' });
  results.push(r.status + ':' + await r.text());
  return results;
};
</script></body></html>`

func TestMain(m *testing.M) {
	if os.Getenv("MYGO_E2E") == "" {
		fmt.Println("skipping e2e tests; set MYGO_E2E=1 to run them in a desktop session")
		os.Exit(0)
	}
	if os.Getenv("MYGO_E2E_QUIT_DURING_DIALOG") == "1" {
		quitDuringDialog()
		return
	}
	mygo.Bind(Greeter{}, probe)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, page)
	})
	mux.HandleFunc("/report.bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="report.bin"`)
		io.WriteString(w, "binary report")
	})
	mux.HandleFunc("POST /echo", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "echo:%s", b)
	})
	if err := mygo.Protocol.Handle("app", mux); err != nil {
		panic(err)
	}
	mygo.App.OnWindowAllClosed(func() {})
	// A window requested from a goroutine before Run waits for the app to
	// be ready (TestEarlyWindow).
	go func() { earlyWindow <- mygo.NewWindow(mygo.WindowOptions{Hidden: true, Title: "early"}) }()
	code := 1
	mygo.App.WhenReady(func() {
		go func() {
			code = m.Run()
			mygo.App.Quit()
		}()
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

// quitDuringDialog is a helper process for TestQuitDuringDialog: it quits
// while an application-modal dialog is open, and exits once Run returns.
func quitDuringDialog() {
	mygo.App.WhenReady(func() {
		go func() {
			res, err := mygo.Dialog.Message(mygo.MessageOptions{Message: "Quitting soon", Buttons: []string{"OK", "Cancel"}})
			fmt.Printf("dialog: %d %v\n", res.Button, err)
		}()
		go func() {
			time.Sleep(500 * time.Millisecond)
			mygo.App.Quit()
		}()
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println("run returned")
}

func TestQuitDuringDialog(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "MYGO_E2E_QUIT_DURING_DIALOG=1")
	var out strings.Builder
	cmd.Stdout = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil || !strings.Contains(out.String(), "run returned") {
			t.Errorf("exit: %v, output %q", err, out.String())
		}
		// The dialog was dismissed with its cancel button.
		if strings.Contains(out.String(), "dialog:") && !strings.Contains(out.String(), "dialog: 1 <nil>") {
			t.Errorf("dialog result: %q", out.String())
		}
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatalf("the app did not quit while a dialog was open; output %q", out.String())
	}
}

// TestIframeCannotCall: the message handler is reachable from iframes, but
// only the page's bridge may call Go.
func TestIframeCannotCall(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Hidden: true})
	forged := `{"t":"call","id":1,"k":"x","m":"Probe.Touch","a":[]}`
	frame := `<script>
const h = window.webkit && webkit.messageHandlers && webkit.messageHandlers.mygo;
if (h) h.postMessage(` + "`" + forged + "`" + `);
parent.postMessage(h ? "posted" : "no handler", "*");
</script>`
	src, _ := json.Marshal(frame)
	w.LoadHTML(`<p>main</p><script>
addEventListener("message", (e) => { window.frameResult = e.data });
const f = document.createElement("iframe");
f.src = "data:text/html," + encodeURIComponent(`+string(src)+`);
document.body.append(f);
</script>`, "app://localhost/")
	waitFor(t, w, "window.frameResult")
	result, _ := mygo.EvalAs[string](w, "window.frameResult")
	if result != "posted" {
		t.Skipf("the iframe had no message handler (%q)", result)
	}
	time.Sleep(200 * time.Millisecond)
	if n := probe.n.Load(); n != 0 {
		t.Fatalf("a call forged by an iframe ran %d times", n)
	}
	if _, err := w.Eval("mygo.call('Probe.Touch')"); err != nil || probe.n.Load() != 1 {
		t.Errorf("the page's own call: %v, count %d", err, probe.n.Load())
	}
	probe.n.Store(0)
}

func TestPopupMenu(t *testing.T) {
	if _, ok := dismissPopups(); !ok {
		t.Skip("popup automation not available on this platform")
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{{Label: "One"}, {Label: "Two"}})
	popup := func(show func()) (shown int) {
		t.Helper()
		done := make(chan struct{})
		go func() { show(); close(done) }()
		time.Sleep(300 * time.Millisecond)
		shown, _ = dismissPopups()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("the popup did not return")
		}
		return shown
	}
	// Without a window GTK cannot place the menu: it must not block.
	if n := popup(func() { menu.Popup(nil) }); n != 0 {
		t.Errorf("popup without a window: %d menus shown", n)
	}
	w := newWindow(t, mygo.WindowOptions{Width: 300, Height: 200})
	w.LoadHTML("<p>menu</p>", "")
	waitFor(t, w, "document.readyState === 'complete'")
	if n := popup(func() { menu.PopupAt(w, 20, 20) }); n != 1 {
		t.Errorf("PopupAt: %d menus shown", n)
	}
}

var earlyWindow = make(chan *mygo.Window, 1)

func TestEarlyWindow(t *testing.T) {
	select {
	case w := <-earlyWindow:
		defer w.Destroy()
		if w.Title() != "early" {
			t.Errorf("title = %q", w.Title())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the window requested before Run was not created")
	}
}

// waitFor polls the page until expr is truthy.
func waitFor(t *testing.T, w *mygo.Window, expr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok, _ := mygo.EvalAs[bool](w, "!!("+expr+")"); ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", expr)
}

func newWindow(t *testing.T, opts mygo.WindowOptions) *mygo.Window {
	t.Helper()
	w := mygo.NewWindow(opts)
	t.Cleanup(w.Destroy)
	return w
}

func TestIPCAndProtocol(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "IPC", Width: 400, Height: 300})
	var dom atomic.Bool
	w.OnDOMReady(func() { dom.Store(true) })
	if err := w.LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, "window.run")
	got, err := mygo.EvalAs[[]any](w, "run()")
	if err != nil {
		t.Fatal(err)
	}
	want := "[Hello, e2e! 2.5 CallError:division by zero true 200:echo:ping]"
	if fmt.Sprint(got) != want {
		t.Errorf("results = %v, want %v", got, want)
	}
	if !dom.Load() {
		t.Error("OnDOMReady not called")
	}
	for i := 1; i <= 3; i++ {
		if err := ticked.Emit(w, Tick{N: i}); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, w, "results.includes('tick3')")
	ticks, _ := mygo.EvalAs[string](w, "results.filter(r => String(r).startsWith('tick')).join(',')")
	if ticks != "tick1,tick2,tick3" {
		t.Errorf("events arrived as %q", ticks)
	}
	if title := w.Title(); title != "E2E" {
		t.Errorf("window title should follow the page, got %q", title)
	}
	if u := w.URL(); u != "app://localhost/" {
		t.Errorf("URL = %q", u)
	}
}

func TestEvalForms(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Hidden: true})
	w.LoadHTML("<p>eval</p>", "")
	waitFor(t, w, "document.readyState === 'complete'")
	cases := []struct{ code, want string }{
		{"1 + 1", "2"},
		{"document.querySelector('p').textContent;", "eval"},
		{"const xs = [1, 2]; return xs.map(x => x * 3)", "[3 6]"},
		{"new Promise(r => setTimeout(() => r({ok: true}), 10))", "map[ok:true]"},
		{"undefined", "<nil>"},
	}
	for _, c := range cases {
		v, err := w.Eval(c.code)
		if err != nil || fmt.Sprint(v) != c.want {
			t.Errorf("Eval(%q) = %v, %v; want %s", c.code, v, err, c.want)
		}
	}
	var evalErr *mygo.EvalError
	if _, err := w.Eval("throw new Error('boom')"); !errors.As(err, &evalErr) || evalErr.Message != "boom" {
		t.Errorf("Eval(throw) = %v", err)
	}
}

func TestVibrancy(t *testing.T) {
	for _, material := range []mygo.Vibrancy{mygo.VibrancySidebar, mygo.VibrancyMica, "no-such-material"} {
		w := newWindow(t, mygo.WindowOptions{Width: 300, Height: 200, Vibrancy: material, Transparent: true})
		w.LoadHTML("<p>vibrancy</p>", "")
		waitFor(t, w, "document.readyState === 'complete'")
		if attached, ok := webViewAttached(w); ok && !attached {
			t.Errorf("vibrancy %q: the page is not in the window", material)
		}
	}
}

func TestWindowGeometryAndState(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Width: 500, Height: 400, X: 100, Y: 120})
	b := w.Bounds()
	if b.Width != 500 || b.Height != 400 || b.X != 100 || b.Y != 120 {
		t.Errorf("bounds = %+v", b)
	}
	w.SetBounds(mygo.Rectangle{X: 150, Y: 160, Width: 420, Height: 320})
	if b := w.Bounds(); b != (mygo.Rectangle{X: 150, Y: 160, Width: 420, Height: 320}) {
		t.Errorf("SetBounds -> %+v", b)
	}
	cw, ch := w.ContentSize()
	widthOK := cw == 420
	if runtime.GOOS == "windows" {
		widthOK = cw > 380 && cw <= 420 // the bounds include the resize borders
	}
	if !widthOK || ch > 320 || (runtime.GOOS != "linux" && ch == 320) {
		t.Errorf("content size = %dx%d, expected the title bar to be excluded", cw, ch)
	}
	w.SetTitle("Renamed")
	if w.Title() != "Renamed" {
		t.Errorf("title = %q", w.Title())
	}
	if !w.IsVisible() {
		t.Error("window should be visible")
	}
	w.Hide()
	if w.IsVisible() {
		t.Error("Hide")
	}
	w.Show()
	w.SetAlwaysOnTop(true)
	if !w.IsAlwaysOnTop() {
		t.Error("SetAlwaysOnTop")
	}
	w.SetOpacity(0.5)
	if o := w.Opacity(); o < 0.49 || o > 0.51 {
		t.Errorf("opacity = %v", o)
	}
	w.SetZoomFactor(1.5)
	if z := w.ZoomFactor(); z != 1.5 {
		t.Errorf("zoom = %v", z)
	}
}

// eventually waits for cond, which polls the window system.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWindowState(t *testing.T) {
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	defer mygo.App.SetPath(mygo.PathUserData, "")
	closeWindow := func(w *mygo.Window) {
		closed := make(chan struct{})
		w.OnClosed(func() { close(closed) })
		w.Close()
		<-closed
	}

	w := mygo.NewWindow(mygo.WindowOptions{StateKey: "e2e", X: 140, Y: 160, Width: 480, Height: 360})
	want := mygo.Rectangle{X: 170, Y: 180, Width: 520, Height: 380}
	w.SetBounds(want)
	closeWindow(w)
	w = newWindow(t, mygo.WindowOptions{StateKey: "e2e", Width: 300, Height: 200})
	if b := w.Bounds(); b != want {
		t.Errorf("restored bounds = %+v, want %+v", b, want)
	}

	if runtime.GOOS == "linux" {
		t.Skip("maximizing needs a window manager, which Xvfb lacks")
	}
	w = mygo.NewWindow(mygo.WindowOptions{StateKey: "e2e-max", Maximized: true, Width: 500, Height: 400})
	eventually(t, "a maximized window", w.IsMaximized)
	closeWindow(w)
	w = newWindow(t, mygo.WindowOptions{StateKey: "e2e-max"})
	eventually(t, "a restored maximized window", w.IsMaximized)
	w.Unmaximize()
	eventually(t, "the normal size", func() bool {
		b := w.Bounds()
		return b.Width == 500 && b.Height == 400
	})
}

// TestFileDrop drops files with synthetic DOM events, the platform side
// being played by a test hook, and checks that the page's own drag and
// drop is undisturbed.
func TestFileDrop(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "Drop", Width: 400, Height: 300})
	drops := make(chan *mygo.FileDropEvent, 4)
	w.OnFileDrop(func(e *mygo.FileDropEvent) { drops <- e })
	w.LoadHTML(`<body style="margin:0;height:300px"><div id=zone style="height:100px"></div><div id=item draggable=true>item</div><script>
window.log = [];
mygo.on("mygo:file-drop", (p) => log.push("event:" + p.paths.join(",") + "@" + p.x + "," + p.y));
zone.addEventListener("dragover", (e) => { e.preventDefault(); log.push("zone:dragover"); });
zone.addEventListener("drop", (e) => { e.preventDefault(); log.push("zone:drop:" + [...e.dataTransfer.files].map((f) => f.name)); });
document.addEventListener("dragover", (e) => log.push("document:dragover:" + e.defaultPrevented));
item.addEventListener("dragstart", () => log.push("item:dragstart"));
// Dispatches a drag event carrying a file at x, y and reports whether it
// was canceled.
window.drag = (target, type, x, y) => {
  const dt = new DataTransfer();
  dt.items.add(new File(["hi"], "dropped.txt"));
  return !target.dispatchEvent(new DragEvent(type, { dataTransfer: dt, bubbles: true, cancelable: true, clientX: x, clientY: y }));
};
</script></body>`, "")
	waitFor(t, w, "window.drag")
	expectDrop := func(want bool, what string) *mygo.FileDropEvent {
		t.Helper()
		select {
		case e := <-drops:
			if !want {
				t.Errorf("%s: unexpected OnFileDrop %+v", what, e)
			}
			return e
		case <-time.After(time.Second):
			if want {
				t.Fatalf("%s: OnFileDrop was not called", what)
			}
		}
		return nil
	}
	eval := func(js string) any {
		t.Helper()
		v, err := w.Eval(js)
		if err != nil {
			t.Fatalf("%s: %v", js, err)
		}
		return v
	}
	if !setDroppedFiles(w, nil) {
		t.Skip("no drop hook on this platform")
	}

	// Dropped on the page's drop zone: the page handles it as usual, and Go
	// gets the path.
	path := filepath.Join(t.TempDir(), "dropped.txt")
	setDroppedFiles(w, []string{path})
	eval(`[drag(zone, "dragover", 20, 30), drag(zone, "drop", 20, 30)]`)
	if e := expectDrop(true, "drop zone"); len(e.Paths) != 1 || e.Paths[0] != path || e.X != 20 || e.Y != 30 {
		t.Errorf("drop zone: %+v", e)
	}
	waitFor(t, w, `log.includes("event:`+strings.ReplaceAll(path, `\`, `\\`)+`@20,30")`)
	if got := eval(`log.slice(0, 3).join(" ")`); got != "zone:dragover document:dragover:true zone:drop:dropped.txt" {
		t.Errorf("the page's drag and drop: %v", got)
	}

	// Elsewhere, the page's listeners see the events untouched, and the
	// bridge then accepts the files instead of letting the engine open them.
	setDroppedFiles(w, []string{path})
	if canceled := eval(`(log.length = 0, drag(document.body, "dragover", 50, 200))`); canceled != true {
		t.Error("an unhandled file drag was not accepted")
	}
	if got := eval(`log.join(" ")`); got != "document:dragover:false" {
		t.Errorf("the page saw %v", got)
	}
	if canceled := eval(`drag(document.body, "drop", 50, 200)`); canceled != true {
		t.Error("an unhandled file drop was not canceled")
	}
	expectDrop(true, "unhandled area")

	// A drag that starts in the page is left alone.
	setDroppedFiles(w, []string{path})
	eval(`item.dispatchEvent(new DragEvent("dragstart", { dataTransfer: new DataTransfer(), bubbles: true }))`)
	if canceled := eval(`drag(document.body, "dragover", 50, 200)`); canceled != false {
		t.Error("an in-page drag was accepted by the bridge")
	}
	if canceled := eval(`drag(document.body, "drop", 50, 200)`); canceled != false {
		t.Error("an in-page drop was canceled by the bridge")
	}
	expectDrop(false, "in-page drag")
	eval(`item.dispatchEvent(new DragEvent("dragend", { bubbles: true }))`)
	setDroppedFiles(w, nil)
}

// TestWindowExtras runs the taskbar and Dock features on the real backends.
func TestWindowExtras(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "Extras", Width: 300, Height: 200, SkipTaskbar: true})
	for _, p := range []mygo.ProgressBar{{Value: 0.3}, {State: mygo.ProgressIndeterminate}, {State: mygo.ProgressError, Value: 0.8}, {}} {
		w.SetProgressBar(p)
	}
	if _, ok := dockTileImage(); ok {
		// The Dock draws the tile offscreen: the bar must show there.
		w.SetProgressBar(mygo.ProgressBar{Value: 0.5})
		data, _ := dockTileImage()
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("Dock tile: %v", err)
		}
		b := img.Bounds()
		blue := func(x, y int) bool {
			r, g, bl, _ := img.At(x, y).RGBA()
			return bl>>8 > 200 && r>>8 < 120 && g>>8 > 100 && g>>8 < 200
		}
		y := b.Max.Y - b.Dy()*13/128 // the middle of the bar
		if !blue(b.Min.X+b.Dx()/5, y) || blue(b.Max.X-b.Dx()/5, y) {
			t.Errorf("the Dock tile does not show half a progress bar")
		}
		w.SetProgressBar(mygo.ProgressBar{})
		if data, _ := dockTileImage(); data != nil {
			t.Error("the Dock tile still shows progress")
		}
	}
	w.FlashFrame(true)
	w.FlashFrame(false)
	w.SetSkipTaskbar(false)
	w.SetSkipTaskbar(true)
	w.SetVisibleOnAllWorkspaces(true)
	w.SetVisibleOnAllWorkspaces(false)
	var icon bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{200, 40, 40, 255}}, image.Point{}, draw.Src)
	if err := png.Encode(&icon, img); err != nil {
		t.Fatal(err)
	}
	if err := w.SetIcon(icon.Bytes()); err != nil {
		t.Errorf("SetIcon: %v", err)
	}
	if err := w.SetIcon(nil); err != nil {
		t.Errorf("SetIcon(nil): %v", err)
	}
	if err := w.SetIcon([]byte("not a png")); err == nil && runtime.GOOS != "darwin" {
		t.Error("SetIcon accepted a bad image")
	}
	if w.Title() != "Extras" {
		t.Error("the window broke")
	}
}

func TestURLScheme(t *testing.T) {
	const scheme = "mygo-e2e"
	switch runtime.GOOS {
	case "darwin":
		// The test binary has no Info.plist to declare the scheme in.
		if err := mygo.App.RegisterURLScheme(scheme); err == nil || !strings.Contains(err.Error(), "urlSchemes") {
			t.Errorf("RegisterURLScheme of an undeclared scheme: %v", err)
		}
		if mygo.App.IsURLSchemeRegistered(scheme) {
			t.Error("an undeclared scheme is registered")
		}
		return
	}
	// GLib reads the XDG directories once: it only sees the handler where
	// the runner points them, as the container of the GUI tests does.
	// Elsewhere the handler is kept out of the user's configuration.
	glib := os.Getenv("XDG_DATA_HOME") != "" && os.Getenv("XDG_CONFIG_HOME") != ""
	if runtime.GOOS == "linux" && !glib {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	}
	if mygo.App.IsURLSchemeRegistered(scheme) {
		t.Fatal("registered before RegisterURLScheme")
	}
	if err := mygo.App.RegisterURLScheme(scheme); err != nil {
		t.Fatal(err)
	}
	defer mygo.App.UnregisterURLScheme(scheme)
	if !mygo.App.IsURLSchemeRegistered(scheme) {
		t.Error("not registered after RegisterURLScheme")
	}
	if runtime.GOOS == "linux" {
		entries, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_DATA_HOME"), "applications", "*.url-handler.desktop"))
		mimeapps, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "mimeapps.list"))
		if len(entries) != 1 || !strings.Contains(string(mimeapps), "x-scheme-handler/"+scheme+"="+filepath.Base(entries[0])) {
			t.Errorf("handler %q, mimeapps.list:\n%s", entries, mimeapps)
		} else if entry, _ := os.ReadFile(entries[0]); !strings.Contains(string(entry), "MimeType=x-scheme-handler/"+scheme+";") || !strings.Contains(string(entry), `" %u`) {
			t.Errorf("handler entry:\n%s", entry)
		} else if _, ok := defaultURLHandler(scheme); ok && glib {
			// GLib, like xdg-open, opens the scheme with the handler.
			eventually(t, "GLib to find the handler", func() bool {
				id, _ := defaultURLHandler(scheme)
				return id == filepath.Base(entries[0])
			})
		}
	}
	if err := mygo.App.UnregisterURLScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if mygo.App.IsURLSchemeRegistered(scheme) {
		t.Error("still registered after UnregisterURLScheme")
	}
}

func TestOpenAtLogin(t *testing.T) {
	if mygo.App.WasOpenedAtLogin() {
		t.Error("the tests were not opened at login")
	}
	if runtime.GOOS == "darwin" {
		// Only app bundles can be login items: nothing is registered.
		if err := mygo.App.SetOpenAtLogin(true); err == nil || !strings.Contains(err.Error(), "bundle") {
			t.Errorf("SetOpenAtLogin without a bundle: %v", err)
		}
		if mygo.App.OpenAtLogin() {
			t.Error("the test binary opens at login")
		}
		return
	}
	if runtime.GOOS == "linux" {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	}
	if mygo.App.OpenAtLogin() {
		t.Fatal("opens at login before SetOpenAtLogin")
	}
	if err := mygo.App.SetOpenAtLogin(true); err != nil {
		t.Fatal(err)
	}
	defer mygo.App.SetOpenAtLogin(false)
	if !mygo.App.OpenAtLogin() {
		t.Error("does not open at login after SetOpenAtLogin")
	}
	if runtime.GOOS == "linux" {
		entries, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "autostart", "*.desktop"))
		if len(entries) != 1 {
			t.Fatalf("autostart entries: %q", entries)
		}
		entry, _ := os.ReadFile(entries[0])
		if !strings.Contains(string(entry), `" --mygo-opened-at-login`) {
			t.Errorf("autostart entry:\n%s", entry)
		}
		// Turned off in the desktop's settings.
		if err := os.WriteFile(entries[0], append(entry, "Hidden=true\n"...), 0o644); err != nil {
			t.Fatal(err)
		}
		if mygo.App.OpenAtLogin() {
			t.Error("a hidden autostart entry opens the app")
		}
	}
	if err := mygo.App.SetOpenAtLogin(false); err != nil {
		t.Fatal(err)
	}
	if mygo.App.OpenAtLogin() {
		t.Error("still opens at login after SetOpenAtLogin(false)")
	}
}

func TestPower(t *testing.T) {
	off := mygo.Power.OnSuspend(func() {})
	defer off()
	const reason = "MyGo e2e keeps the display on"
	release := mygo.Power.KeepAwake(reason, true)
	assertions := func() string {
		out, _ := exec.Command("pmset", "-g", "assertions").Output()
		return string(out)
	}
	if runtime.GOOS == "darwin" && !strings.Contains(assertions(), reason) {
		t.Error("KeepAwake made no power assertion")
	}
	release()
	if runtime.GOOS == "darwin" {
		eventually(t, "the assertion to go", func() bool { return !strings.Contains(assertions(), reason) })
	}
	t.Logf("on battery: %v, idle: %v", mygo.Power.IsOnBattery(), mygo.Power.IdleTime())
	if idle := mygo.Power.IdleTime(); idle < 0 || runtime.GOOS == "darwin" && idle == 0 {
		t.Errorf("idle time = %v", idle)
	}
}

func TestDockMenu(t *testing.T) {
	clicked := make(chan struct{}, 1)
	mygo.App.Dock.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "New Window", Click: func(*mygo.MenuItem, *mygo.Window) { clicked <- struct{}{} }},
		mygo.Separator(),
		{Label: "Settings"},
	}))
	defer mygo.App.Dock.SetMenu(nil)
	titles, ok := dockMenu(0)
	if !ok {
		t.Skip("no Dock on this platform")
	}
	if len(titles) != 3 || titles[0] != "New Window" || titles[2] != "Settings" {
		t.Errorf("Dock menu = %q", titles)
	}
	select {
	case <-clicked:
	case <-time.After(3 * time.Second):
		t.Error("clicking the Dock menu item did not reach its handler")
	}
	mygo.App.Dock.SetMenu(nil)
	if titles, _ := dockMenu(-1); titles != nil {
		t.Errorf("Dock menu after SetMenu(nil) = %q", titles)
	}
}

func TestPrintToPDF(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "PDF", Width: 400, Height: 300})
	w.LoadHTML(`<style>p + p { page-break-before: always }</style><p>one</p><p>two</p><p>three</p>`, "")
	waitFor(t, w, `document.readyState === "complete" && document.querySelectorAll("p").length === 3`)
	pages := regexp.MustCompile(`/Type\s*/Page[^s]`)
	mediaBox := regexp.MustCompile(`/MediaBox\s*\[\s*0\s+0\s+([\d.]+)\s+([\d.]+)`)
	for _, landscape := range []bool{false, true} {
		pdf, err := w.PrintToPDF(mygo.PDFOptions{PageSize: mygo.PageA4, Landscape: landscape})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
			t.Fatalf("not a PDF: %.20q", pdf)
		}
		if n := len(pages.FindAll(pdf, -1)); n != 3 {
			t.Errorf("landscape=%v: %d pages, want 3", landscape, n)
		}
		m := mediaBox.FindSubmatch(pdf)
		if m == nil {
			t.Fatal("no page size in the PDF")
		}
		width, _ := strconv.ParseFloat(string(m[1]), 64)
		height, _ := strconv.ParseFloat(string(m[2]), 64)
		want := [2]float64{595, 842} // A4 in points
		if landscape {
			want = [2]float64{842, 595}
		}
		if math.Abs(width-want[0]) > 2 || math.Abs(height-want[1]) > 2 {
			t.Errorf("landscape=%v: page size %vx%v, want %v", landscape, width, height, want)
		}
	}
}

func TestPermissions(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "Permissions", Width: 300, Height: 200})
	// A secure context: notifications are not for opaque origins.
	if err := w.LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	origin := "app://localhost"
	if runtime.GOOS == "windows" {
		origin = "https://app.localhost" // how WebView2 serves custom schemes
	}
	waitFor(t, w, `location.origin === "`+origin+`" && document.readyState === "complete"`)
	// The app's own pages are secure contexts with a real origin, which
	// secure-context APIs (camera, Web Crypto) and storage need.
	if got := mustEval(t, w, `[window.isSecureContext, typeof crypto.subtle].join(" ")`); got != "true object" {
		t.Errorf("%s: %v", origin, got)
	}
	if ok, _ := mygo.EvalAs[bool](w, `typeof Notification !== "undefined" && !!Notification.requestPermission`); !ok {
		t.Skip("the engine has no Notification API")
	}
	asked := make(chan mygo.PermissionRequest, 4)
	w.SetPermissionHandler(func(req mygo.PermissionRequest) bool {
		asked <- req
		return true
	})
	got, err := mygo.EvalAs[string](w, `Notification.requestPermission()`)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case req := <-asked:
		if len(req.Permissions) != 1 || req.Permissions[0] != mygo.PermissionNotifications {
			t.Errorf("request = %+v", req)
		}
		if got != "granted" {
			t.Errorf("the page got %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Skipf("the engine decided alone (%q)", got)
	}
}

func mustEval(t *testing.T, w *mygo.Window, js string) any {
	t.Helper()
	v, err := w.Eval(js)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestDownloads(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "Downloads", Width: 300, Height: 200})
	if err := w.LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, `window.run`)
	dir := t.TempDir()
	started := make(chan *mygo.DownloadEvent, 2)
	done := make(chan *mygo.Download, 2)
	w.OnWillDownload(func(e *mygo.DownloadEvent) {
		if e.Path == "" || filepath.Base(e.Path) != e.SuggestedName {
			t.Errorf("default path %q for %q", e.Path, e.SuggestedName)
		}
		e.Path = filepath.Join(dir, e.SuggestedName)
		started <- e
	})
	w.OnDownloadDone(func(d *mygo.Download) { done <- d })
	check := func(what, name, content string) {
		t.Helper()
		select {
		case e := <-started:
			if e.SuggestedName != name {
				t.Errorf("%s: suggested name %q, want %q", what, e.SuggestedName, name)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: OnWillDownload was not called", what)
		}
		select {
		case d := <-done:
			if d.Err != nil {
				t.Fatalf("%s: %v", what, d.Err)
			}
			if b, err := os.ReadFile(d.Path); err != nil || string(b) != content {
				t.Errorf("%s: saved %q, %v", what, b, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: OnDownloadDone was not called", what)
		}
	}
	// A link with the download attribute.
	mustEval(t, w, `(() => { const a = document.createElement("a"); a.href = "data:text/csv,a%2Cb"; a.download = "report.csv"; document.body.append(a); a.click(); return true })()`)
	check("download link", "report.csv", "a,b")
	// A response sent as an attachment.
	mustEval(t, w, `(location.href = "report.bin", true)`)
	check("attachment", "report.bin", "binary report")
}

func TestClearBrowsingData(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "Data", Width: 300, Height: 200})
	if err := w.LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, `window.run`)
	mustEval(t, w, `(localStorage.setItem("token", "secret"), window.oldPage = true)`)
	if err := mygo.App.ClearBrowsingData(); err != nil {
		t.Fatal(err)
	}
	w.Reload()
	waitFor(t, w, `window.run && !window.oldPage`)
	if got := mustEval(t, w, `localStorage.getItem("token")`); got != nil {
		t.Errorf("after ClearBrowsingData the token is %v", got)
	}
}

func TestFindInPage(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "Find", Width: 400, Height: 300})
	w.LoadHTML(`<p>MyGo is a framework. mygo apps are small.</p><p style="display:none">mygo hidden</p><p>Say mygo</p>`, "")
	waitFor(t, w, `document.readyState === "complete" && document.querySelectorAll("p").length === 3`)
	find := func(text string, opts mygo.FindOptions) mygo.FindResult {
		t.Helper()
		res, err := w.FindInPage(text, opts)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := find("mygo", mygo.FindOptions{}); res != (mygo.FindResult{Matches: 3, Active: 1}) {
		t.Errorf("find = %+v, want 3 visible matches", res)
	}
	if res := find("mygo", mygo.FindOptions{FindNext: true}); res.Active != 2 {
		t.Errorf("next = %+v", res)
	}
	if res := find("mygo", mygo.FindOptions{FindNext: true, Backward: true}); res.Active != 1 {
		t.Errorf("previous = %+v", res)
	}
	if res := find("mygo", mygo.FindOptions{FindNext: true, Backward: true}); res.Active != 3 {
		t.Errorf("previous wraps around: %+v", res)
	}
	if res := find("MyGo", mygo.FindOptions{MatchCase: true}); res.Matches != 1 {
		t.Errorf("match case = %+v", res)
	}
	if res := find("nothing", mygo.FindOptions{}); res != (mygo.FindResult{}) {
		t.Errorf("no match = %+v", res)
	}
	w.StopFindInPage()
}

func TestGlobalShortcut(t *testing.T) {
	pressed := make(chan struct{}, 1)
	if err := mygo.GlobalShortcut.Register("Ctrl+Shift+K", func() { pressed <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	defer mygo.GlobalShortcut.Unregister("Ctrl+Shift+K")
	if !pressCtrlShiftK() {
		t.Skip("no keyboard automation on this platform")
	}
	select {
	case <-pressed:
	case <-time.After(3 * time.Second):
		t.Fatal("the global shortcut was not reported")
	}
}

// TestGlobalShortcutPortal has the desktop bind global shortcuts through
// the XDG desktop portal, as on Wayland, where apps grab no keys.
func TestGlobalShortcutPortal(t *testing.T) {
	restore, ok := usePortalShortcuts()
	if !ok {
		t.Skip("no GlobalShortcuts portal")
	}
	defer restore()
	pressed := make(chan string, 8)
	register := func(acc string) {
		t.Helper()
		if err := mygo.GlobalShortcut.Register(acc, func() { pressed <- acc }); err != nil {
			t.Fatal(err)
		}
	}
	// The desktop binds shortcuts after Register returns, in a new session
	// whenever they change.
	expect := func(acc string, keys ...string) {
		t.Helper()
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
			if !pressKeys(keys...) {
				t.Skip("no keyboard automation")
			}
			select {
			case got := <-pressed:
				if got != acc {
					t.Fatalf("pressing %v reported %s", keys, got)
				}
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
		t.Fatalf("%s was not reported", acc)
	}
	register("Ctrl+Shift+K")
	defer mygo.GlobalShortcut.UnregisterAll()
	expect("Ctrl+Shift+K", "Control_L", "Shift_L", "k")
	register("Alt+Shift+J")
	expect("Alt+Shift+J", "Alt_L", "Shift_L", "j")
	expect("Ctrl+Shift+K", "Control_L", "Shift_L", "k")

	mygo.GlobalShortcut.Unregister("Ctrl+Shift+K")
	expect("Alt+Shift+J", "Alt_L", "Shift_L", "j")
	pressKeys("Control_L", "Shift_L", "k")
	select {
	case got := <-pressed:
		t.Fatalf("%s was reported after it was unregistered", got)
	case <-time.After(time.Second):
	}
}

func TestCloseEvents(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Hidden: true})
	var prevent atomic.Bool
	prevent.Store(true)
	closed := make(chan struct{})
	w.OnClose(func(e *mygo.CloseEvent) {
		if prevent.Load() {
			e.PreventDefault()
		}
	})
	w.OnClosed(func() { close(closed) })
	w.Close()
	if w.IsDestroyed() {
		t.Fatal("close was not prevented")
	}
	prevent.Store(false)
	w.Close()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("OnClosed not called")
	}
}

func TestCapturePage(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Width: 320, Height: 240})
	w.LoadHTML(`<body style="margin:0;background:rgb(255,0,0)"></body>`, "")
	waitFor(t, w, "document.readyState === 'complete'")
	time.Sleep(200 * time.Millisecond)
	png, err := w.CapturePage()
	if err != nil {
		t.Fatal(err)
	}
	if len(png) < 100 || !strings.HasPrefix(string(png), "\x89PNG") {
		t.Fatalf("not a PNG (%d bytes)", len(png))
	}
	if out := os.Getenv("MYGO_E2E_SNAPSHOT"); out != "" {
		_ = os.WriteFile(out, png, 0o644)
	}
}

func TestMenuAndClipboard(t *testing.T) {
	clicked := make(chan string, 1)
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "Test", Submenu: []*mygo.MenuItem{
			{ID: "ping", Label: "Ping", Accelerator: "CmdOrCtrl+Shift+P", Click: func(item *mygo.MenuItem, _ *mygo.Window) { clicked <- item.ID }},
			{ID: "check", Label: "Check", Type: mygo.MenuItemCheckbox},
		}},
		{Role: mygo.RoleEditMenu},
	})
	mygo.App.SetMenu(menu)
	defer mygo.App.SetMenu(nil)
	menu.ItemByID("check").SetChecked(true)
	if !menu.ItemByID("check").IsChecked() {
		t.Error("SetChecked")
	}
	mygo.Clipboard.WriteText("mygo clipboard ✓")
	if got := mygo.Clipboard.ReadText(); got != "mygo clipboard ✓" {
		t.Errorf("clipboard = %q", got)
	}
	if ds := mygo.Screen.Displays(); len(ds) == 0 || ds[0].Bounds.Width == 0 {
		t.Errorf("displays = %+v", ds)
	}
}

func TestWindowOpenHandler(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Hidden: true})
	opened := make(chan string, 1)
	w.SetWindowOpenHandler(func(req mygo.WindowOpenRequest) *mygo.WindowOptions {
		opened <- req.URL
		return nil
	})
	w.LoadHTML(`<a id="l" href="https://example.com/x" target="_blank">x</a>`, "https://example.com/")
	waitFor(t, w, "document.getElementById('l')")
	if _, err := w.Eval("window.open('https://example.com/popup')"); err != nil {
		t.Fatal(err)
	}
	select {
	case u := <-opened:
		if u != "https://example.com/popup" {
			t.Errorf("opened %q", u)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("window open handler not called")
	}
}

func TestMenuActivation(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Width: 400, Height: 300})
	w.LoadHTML("<p>menus</p>", "")
	clicks := make(chan string, 4)
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "Test", Submenu: []*mygo.MenuItem{
			{ID: "ping", Label: "Ping", Accelerator: "CmdOrCtrl+Shift+Y", Click: func(item *mygo.MenuItem, win *mygo.Window) {
				clicks <- item.ID
			}},
			{ID: "check", Label: "Check", Type: mygo.MenuItemCheckbox},
			{ID: "checked", Label: "Checked", Type: mygo.MenuItemCheckbox, Checked: true, Click: func(item *mygo.MenuItem, win *mygo.Window) {
				clicks <- item.ID
			}},
		}},
	})
	mygo.App.SetMenu(menu)
	defer mygo.App.SetMenu(nil)
	w.Focus()
	time.Sleep(100 * time.Millisecond)

	// Building the menu and changing state from code are not clicks.
	menu.ItemByID("checked").SetChecked(false)
	menu.ItemByID("checked").SetChecked(true)
	select {
	case id := <-clicks:
		t.Fatalf("%q clicked without user action", id)
	case <-time.After(300 * time.Millisecond):
	}
	if !menu.ItemByID("checked").IsChecked() {
		t.Fatal("checked item lost its state")
	}

	if err := activateMenu(w, "Test", "Ping"); err != nil {
		t.Fatal(err)
	}
	expectClick(t, clicks, "ping")
	if err := activateMenu(w, "Test", "Check"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !menu.ItemByID("check").IsChecked() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !menu.ItemByID("check").IsChecked() {
		t.Error("checkbox not toggled by activation")
	}
	if handled, ok := pressShortcut("y", true); ok {
		if !handled {
			t.Error("key equivalent not handled by the menu")
		}
		expectClick(t, clicks, "ping")
	}
}

func expectClick(t *testing.T, clicks chan string, want string) {
	t.Helper()
	select {
	case got := <-clicks:
		if got != want {
			t.Errorf("clicked %q, want %q", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("menu item %q was not clicked", want)
	}
}

func TestJavaScriptAlert(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Width: 400, Height: 300})
	w.LoadHTML("<p>alert</p>", "")
	waitFor(t, w, "document.readyState === 'complete'")
	if _, ok := endSheet(w); !ok {
		t.Skip("dialog automation not available on this platform")
	}
	// alert() blocks the page until the sheet is dismissed.
	if _, err := w.Eval("setTimeout(() => { alert('hello'); window.alertDone = true }, 0)"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok, _ := endSheet(w); ok {
			waitFor(t, w, "window.alertDone")
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("alert sheet never appeared")
}

func TestWindowOpenAllowed(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Width: 400, Height: 300})
	created := make(chan *mygo.Window, 1)
	off := mygo.App.OnWindowCreated(func(c *mygo.Window) { created <- c })
	defer off()
	w.SetWindowOpenHandler(func(req mygo.WindowOpenRequest) *mygo.WindowOptions {
		return &mygo.WindowOptions{Width: 320, Height: 240}
	})
	if err := w.LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, "window.run")
	if _, err := w.Eval("void window.open('app://localhost/?child=1')"); err != nil {
		t.Fatal(err)
	}
	var child *mygo.Window
	select {
	case child = <-created:
	case <-time.After(5 * time.Second):
		t.Fatal("child window not created")
	}
	defer child.Destroy()
	waitFor(t, child, "window.run && location.search === '?child=1'")
	// The child has its own bridge: calls identify the child window.
	same, err := mygo.EvalAs[bool](child, "mygo.call('Greeter.WindowID').then(id => id === mygo.windowId)")
	if err != nil || !same {
		t.Errorf("call from child: %v, %v", same, err)
	}
	if id, _ := mygo.EvalAs[int](child, "mygo.windowId"); id != child.ID() {
		t.Errorf("child bridge reports window %d, want %d", id, child.ID())
	}
	// On Linux new windows are independent pages (see Window.SetWindowOpenHandler).
	if opener, _ := mygo.EvalAs[bool](child, "window.opener !== null"); !opener && runtime.GOOS == "darwin" {
		t.Error("window.opener is not set")
	}
}

// TestClick is a regression test: clicking the page used to crash on macOS.
func TestClick(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Width: 400, Height: 300})
	w.LoadHTML(`<body style="margin:0"><button id="b" style="width:200px;height:100px" onclick="window.clicked=(window.clicked||0)+1">x</button></body>`, "")
	waitFor(t, w, "document.readyState === 'complete'")
	if !click(w, 50, 50) {
		t.Skip("click automation not available on this platform")
	}
	click(w, 60, 40)
	waitFor(t, w, "window.clicked === 2")

	// Clicking a drag region of a frameless window starts a native drag.
	f := newWindow(t, mygo.WindowOptions{Width: 400, Height: 300, Frameless: true})
	f.LoadHTML(`<body style="margin:0"><div id="bar" style="--app-region:drag;height:40px" onmousedown="window.pressed=true"></div></body>`, "")
	waitFor(t, f, "document.readyState === 'complete'")
	click(f, 100, 20)
	waitFor(t, f, "window.pressed === true")
	if !f.IsVisible() {
		t.Error("frameless window disappeared after a drag click")
	}
}
