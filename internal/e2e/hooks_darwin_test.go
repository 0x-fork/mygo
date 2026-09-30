//go:build darwin

package e2e

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/darwin"
)

func activateMenu(w *mygo.Window, path ...string) (err error) {
	mygo.RunOnMain(func() { err = darwin.TestPerformMenuItem(path...) })
	return err
}

func pressShortcut(key string, shift bool) (handled bool, supported bool) {
	mygo.RunOnMain(func() { handled = darwin.TestPerformKeyEquivalent(key, shift) })
	return handled, true
}

func endSheet(w *mygo.Window) (ok bool, supported bool) {
	mygo.RunOnMain(func() { ok = darwin.TestEndSheet(w.NativeHandle()) })
	return ok, true
}

func click(w *mygo.Window, x, y float64) bool {
	mygo.RunOnMain(func() { darwin.TestClick(w.NativeHandle(), x, y) })
	return true
}

func webViewAttached(w *mygo.Window) (attached bool, supported bool) {
	mygo.RunOnMain(func() { attached = darwin.TestWebViewAttached(w.NativeHandle()) })
	return attached, true
}

func dockDevTools(w *mygo.Window) (ok bool) {
	mygo.RunOnMain(func() { ok = darwin.TestDockInspector(w.NativeHandle()) })
	return ok
}

func dockedDevToolsPlace(w *mygo.Window) (place string) {
	mygo.RunOnMain(func() { place = darwin.TestInspectorPlace(w.NativeHandle()) })
	return place
}

// Context menus track the mouse in a modal loop; not automated.
func dismissPopups() (int, bool) { return 0, false }

func setDroppedFiles(w *mygo.Window, paths []string) bool {
	mygo.RunOnMain(func() { darwin.TestSetDroppedFiles(w.NativeHandle(), paths) })
	return true
}

func dockTileImage() (png []byte, supported bool) {
	mygo.RunOnMain(func() { png = darwin.TestDockTileImage() })
	return png, true
}

func defaultURLHandler(string) (string, bool) { return "", false }

func dockMenu(click int) (titles []string, supported bool) {
	mygo.RunOnMain(func() { titles = darwin.TestDockMenu(click) })
	return titles, true
}

// Posting key events needs the accessibility permission on macOS.
func pressCtrlShiftK() bool { return false }

// Only Linux binds global shortcuts through a desktop portal.
func usePortalShortcuts() (func(), bool) { return nil, false }

func pressKeys(...string) bool { return false }

func fullScreenHidesToolbar(w *mygo.Window) (hides bool, supported bool) {
	mygo.RunOnMain(func() { hides = darwin.TestFullScreenHidesToolbar(w.NativeHandle()) })
	return hides, true
}

func trafficLights(w *mygo.Window) (x, y float64, supported bool) {
	mygo.RunOnMain(func() { x, y = darwin.TestTrafficLights(w.NativeHandle()) })
	return x, y, true
}

// Frameless windows keep native resize borders here.
func movePointer(int, int) bool                { return false }
func pressButton(bool) bool                    { return false }
func resizeCursor(*mygo.Window) (string, bool) { return "", false }

// The menu bar belongs to the application on macOS.
func menuBarShown(*mygo.Window) (bool, bool)                { return false, false }
func activateAccelerator(*mygo.Window, string) (bool, bool) { return false, false }
func enterMenuBar(*mygo.Window, string) (bool, bool, bool)  { return false, false, false }

// The traffic lights are AppKit's own.
func titleButtons(*mygo.Window) ([]string, bool) { return nil, false }
func pressTitleButton(*mygo.Window, string) bool { return false }

func topNonClient(*mygo.Window) (int32, bool) { return 0, false }
