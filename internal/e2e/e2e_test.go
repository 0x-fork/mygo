// Package e2e drives the real native backend. It needs a desktop session,
// so it only runs with MYGO_E2E=1:
//
//	MYGO_E2E=1 go test ./internal/e2e
package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
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
	if cw != 420 || ch > 320 || (runtime.GOOS == "darwin" && ch == 320) {
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
