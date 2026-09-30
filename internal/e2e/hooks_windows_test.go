//go:build windows && (amd64 || arm64)

package e2e

import (
	"time"

	"github.com/egoist/mygo"
	win "github.com/egoist/mygo/internal/windows"
)

func activateMenu(w *mygo.Window, path ...string) (err error) {
	mygo.RunOnMain(func() { err = win.TestActivateMenuItem(w.NativeHandle(), path...) })
	return err
}

// Keyboard, dialog, click and popup automation are not wired on Windows.
func pressShortcut(string, bool) (bool, bool) { return false, false }

func endSheet(*mygo.Window) (bool, bool) { return false, false }

func click(*mygo.Window, float64, float64) bool { return false }

func webViewAttached(*mygo.Window) (bool, bool) { return false, false }

// WebView2 opens DevTools in a window of its own.
func dockDevTools(*mygo.Window) bool { return false }

func dockedDevToolsPlace(*mygo.Window) string { return "" }

func dismissPopups() (int, bool) { return 0, false }

func setDroppedFiles(w *mygo.Window, paths []string) bool {
	mygo.RunOnMain(func() { win.TestSetDroppedFiles(w.NativeHandle(), paths) })
	return true
}

// The progress bar is shown by the shell, out of the app's reach.
func dockTileImage() ([]byte, bool) { return nil, false }

func defaultURLHandler(string) (string, bool) { return "", false }

func dockMenu(int) ([]string, bool) { return nil, false }

func pressCtrlShiftK() bool {
	mygo.RunOnMain(func() { win.TestPressKeys(0x11, 0x10, 'K') }) // VK_CONTROL, VK_SHIFT
	return true
}

// Only Linux binds global shortcuts through a desktop portal.
func usePortalShortcuts() (func(), bool) { return nil, false }

func pressKeys(...string) bool { return false }

// Only macOS windows have a toolbar.
func fullScreenHidesToolbar(*mygo.Window) (bool, bool) { return false, false }

// Only macOS windows have traffic lights.
func trafficLights(*mygo.Window) (float64, float64, bool) { return 0, 0, false }

// Frameless windows keep native resize borders here.
func movePointer(int, int) bool                { return false }
func pressButton(bool) bool                    { return false }
func resizeCursor(*mygo.Window) (string, bool) { return "", false }

func menuBarShown(w *mygo.Window) (shown, supported bool) {
	mygo.RunOnMain(func() { shown = win.TestMenuBarShown(w.NativeHandle()) })
	return shown, true
}

func activateAccelerator(w *mygo.Window, accel string) (handled, supported bool) {
	mygo.RunOnMain(func() { handled = win.TestActivateAccelerator(w.NativeHandle(), accel) })
	return handled, true
}

// enterMenuBar sends the window the SC_KEYMENU that Alt and F10 become, and
// leaves the menus from inside the menu loop, which runs the app's work.
func enterMenuBar(w *mygo.Window, _ string) (during, after, supported bool) {
	hwnd := w.NativeHandle()
	inLoop := make(chan bool, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		mygo.RunOnMain(func() {
			inLoop <- win.TestMenuBarShown(hwnd)
			win.TestEndMenu()
		})
	}()
	mygo.RunOnMain(func() { supported = win.TestEnterMenuBar(hwnd) })
	if !supported {
		return false, false, false
	}
	during = <-inLoop
	mygo.RunOnMain(func() { after = win.TestMenuBarShown(hwnd) })
	return during, after, true
}

// titleButtons returns the window controls of a hidden title bar, named
// after the hit-test codes over them.
func titleButtons(w *mygo.Window) (names []string, supported bool) {
	mygo.RunOnMain(func() { names = win.TestCaptionButtons(w.NativeHandle()) })
	return names, true
}

func pressTitleButton(w *mygo.Window, name string) (ok bool) {
	mygo.RunOnMain(func() { ok = win.TestPressCaptionButton(w.NativeHandle(), name) })
	return ok
}

// topNonClient returns how many pixels at the top of a window are not its
// page's.
func topNonClient(w *mygo.Window) (px int32, supported bool) {
	mygo.RunOnMain(func() { px = win.TestTopNonClient(w.NativeHandle()) })
	return px, true
}
