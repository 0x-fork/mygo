package mygo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

func TestWindowState(t *testing.T) {
	dir := t.TempDir()
	App.SetPath(PathUserData, dir)
	defer App.SetPath(PathUserData, "")
	defer func(d, s time.Duration) { windowStateDelay, windowStateSaveDelay = d, s }(windowStateDelay, windowStateSaveDelay)
	windowStateDelay, windowStateSaveDelay = 20*time.Millisecond, 20*time.Millisecond
	// A new process: the file is read again.
	relaunch := func() {
		onMain(func() {
			saveWindowStates()
			windowStates.loaded, windowStates.byKey = false, nil
		})
	}
	relaunch()
	file := filepath.Join(dir, windowStateFile)
	saved := func() map[string]savedWindow {
		t.Helper()
		var m map[string]savedWindow
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	waitSaved := func(key string, want savedWindow) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			if data, err := os.ReadFile(file); err == nil {
				var m map[string]savedWindow
				if json.Unmarshal(data, &m) == nil && m[key] == want {
					return
				}
			}
			if time.Now().After(deadline) {
				data, _ := os.ReadFile(file)
				t.Fatalf("%s never saved as %+v: %s", key, want, data)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Nothing saved: the options apply, and the window is remembered once
	// it settled.
	w, fw := testWindow(t, WindowOptions{StateKey: "main", X: 100, Y: 120, Width: 700, Height: 500})
	if o := fw.Opts; o.X != 100 || o.Y != 120 || o.Width != 700 || o.Maximized {
		t.Errorf("first window options = %+v", o)
	}
	waitSaved("main", savedWindow{X: 100, Y: 120, Width: 700, Height: 500})
	onMain(func() { fw.SetBounds(platform.Rect{X: 200, Y: 150, Width: 800, Height: 600}) })
	waitSaved("main", savedWindow{X: 200, Y: 150, Width: 800, Height: 600})
	// Maximized, it keeps its normal bounds.
	onMain(func() {
		fw.Maximize()
		fw.SetBounds(platform.Rect{Y: 25, Width: 1440, Height: 875})
	})
	w.Destroy()
	relaunch()
	if got, want := saved()["main"], (savedWindow{X: 200, Y: 150, Width: 800, Height: 600, Maximized: true}); got != want {
		t.Errorf("saved %+v, want %+v", got, want)
	}

	// The next launch gets it back.
	_, fw = testWindow(t, WindowOptions{StateKey: "main", Width: 400, Height: 300, UseContentSize: true})
	if o := fw.Opts; o.X != 200 || o.Y != 150 || o.Width != 800 || o.Height != 600 || !o.Maximized || o.Center || o.UseContentSize {
		t.Errorf("restored options = %+v", o)
	}

	// Off every display: centered, with its size as far as it fits.
	onMain(func() { windowStates.byKey["gone"] = savedWindow{X: 5000, Y: 100, Width: 2000, Height: 700} })
	_, fw = testWindow(t, WindowOptions{StateKey: "gone"})
	if o := fw.Opts; !o.Center || o.Width != 1440 || o.Height != 700 {
		t.Errorf("off screen options = %+v", o)
	}

	// Created maximized, the requested bounds are its normal ones.
	testWindow(t, WindowOptions{StateKey: "max", Maximized: true, Width: 1000, Height: 700})
	if got, want := onMainValue(func() savedWindow { return windowStates.byKey["max"] }), (savedWindow{X: 220, Y: 112, Width: 1000, Height: 700, Maximized: true}); got != want {
		t.Errorf("maximized at creation: %+v, want %+v", got, want)
	}

	// A damaged file is ignored.
	relaunch()
	if err := os.WriteFile(file, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, fw = testWindow(t, WindowOptions{StateKey: "main", X: 10, Y: 30})
	if o := fw.Opts; o.X != 10 || o.Y != 30 || o.Maximized {
		t.Errorf("options with a damaged file = %+v", o)
	}
}

func TestFileDrop(t *testing.T) {
	w, fw := readyWindow(t, WindowOptions{})
	drops := make(chan *FileDropEvent, 1)
	w.OnFileDrop(func(e *FileDropEvent) { drops <- e })

	fw.Dropped = []string{"/tmp/a.txt", "/tmp/b"}
	page(fw, `{"t":"drop","x":10.4,"y":20}`)
	select {
	case e := <-drops:
		if len(e.Paths) != 2 || e.Paths[0] != "/tmp/a.txt" || e.X != 10 || e.Y != 20 {
			t.Errorf("drop = %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("OnFileDrop was not called")
	}
	// about:blank is the app's own page: it gets the paths too.
	m := received(t, fw, func(m map[string]any) bool { return m["t"] == "event" && m["n"] == "mygo:file-drop" })
	if p := m["p"].(map[string]any); p["x"] != float64(10) || len(p["paths"].([]any)) != 2 {
		t.Errorf("page event = %v", m)
	}

	// A drop without files (text, or a page posting on its own) is ignored.
	page(fw, `{"t":"drop","x":1,"y":2}`)
	select {
	case e := <-drops:
		t.Errorf("unexpected drop %+v", e)
	case <-time.After(50 * time.Millisecond):
	}

	defer func() {
		if recover() == nil {
			t.Error("NewEvent accepted a reserved name")
		}
	}()
	NewEvent[int]("mygo:file-drop")
}

func TestWindowExtras(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{SkipTaskbar: true})
	if !fw.Opts.SkipTaskbar {
		t.Error("SkipTaskbar option not passed")
	}
	for _, c := range []struct {
		p     ProgressBar
		state string
		value float64
	}{
		{ProgressBar{Value: 0.25}, "normal", 0.25},
		{ProgressBar{State: ProgressNormal}, "normal", 0},
		{ProgressBar{State: ProgressError, Value: 2}, "error", 1},
		{ProgressBar{State: ProgressIndeterminate}, "indeterminate", 0},
		{ProgressBar{Value: -1}, "", 0},
		{ProgressBar{}, "", 0},
	} {
		w.SetProgressBar(c.p)
		if st, v := onMainValue(func() string { return fw.Progress }), onMainValue(func() float64 { return fw.ProgressValue }); st != c.state || v != c.value {
			t.Errorf("SetProgressBar(%+v) = %q %v, want %q %v", c.p, st, v, c.state, c.value)
		}
	}
	w.FlashFrame(true)
	w.SetSkipTaskbar(false)
	w.SetVisibleOnAllWorkspaces(true)
	if !onMainValue(func() bool { return fw.Flashing && !fw.SkipsTaskbar && fw.OnAllWorkspaces }) {
		t.Error("window extras not applied")
	}
	if err := w.SetIcon([]byte("not a png")); err == nil {
		t.Error("SetIcon accepted a bad image")
	}
}

func TestPrintToPDFOptions(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{})
	pdf, err := w.PrintToPDF(PDFOptions{})
	if err != nil || string(pdf) != "%PDF-1.4 fake" {
		t.Fatalf("PrintToPDF = %q, %v", pdf, err)
	}
	want := platform.PDFOptions{PageWidth: 8.5, PageHeight: 11, MarginTop: 0.4, MarginRight: 0.4, MarginBottom: 0.4, MarginLeft: 0.4}
	if got := onMainValue(func() platform.PDFOptions { return fw.PDF }); got != want {
		t.Errorf("defaults = %+v, want %+v", got, want)
	}
	w.PrintToPDF(PDFOptions{PageSize: PageA4, Landscape: true, Margins: &Margins{}, Background: true})
	want = platform.PDFOptions{Landscape: true, PageWidth: 8.27, PageHeight: 11.69, Background: true}
	if got := onMainValue(func() platform.PDFOptions { return fw.PDF }); got != want {
		t.Errorf("options = %+v, want %+v", got, want)
	}
}
