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

// Keyboard, dialog and popup automation are not wired on Windows.
func pressShortcut(string, bool) (bool, bool) { return false, false }

func endSheet(*mygo.Window) (bool, bool) { return false, false }

// Clicks reach windows showing native UI only: WebView2 takes the mouse in
// windows of its own process.
func click(w *mygo.Window, x, y float64) (ok bool) {
	mygo.RunOnMain(func() { ok = win.TestClickSurface(w.NativeHandle(), x, y) })
	return ok
}

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
// page's, and how many a hidden title bar keeps there.
func topNonClient(w *mygo.Window) (px, want int32, supported bool) {
	mygo.RunOnMain(func() { px, want = win.TestTopNonClient(w.NativeHandle()) })
	return px, want, true
}

// A Control-click is a secondary click on macOS only.
func controlClick(*mygo.Window, float64, float64) bool { return false }

// composeOver does what an input method that converts typed text again
// does through IMM32: it settles the range it reconverts, then composes or
// commits.
func composeOver(w *mygo.Window, text string, caret int, commit bool, from, length int) (ok bool) {
	mygo.RunOnMain(func() { ok = win.TestComposeOver(w.NativeHandle(), text, caret, commit, from, length) })
	return ok
}

// inputClient returns the selection and the text around it that input
// methods get with IMR_DOCUMENTFEED, which holds no composition.
func inputClient(w *mygo.Window) (selected [2]int, document string, ok bool) {
	mygo.RunOnMain(func() {
		var start, length int
		document, start, length, ok = win.TestDocumentFeed(w.NativeHandle())
		selected = [2]int{start, length}
	})
	return selected, document, ok
}

func dropFiles(w *mygo.Window, x, y float64, paths []string) (over, dropped, ok bool) {
	mygo.RunOnMain(func() { over, dropped = win.TestDropFiles(w.NativeHandle(), x, y, paths) })
	return over, dropped, true
}

// The control types of elements in UI Automation.
const roleText, roleButton, roleCheckBox, roleTextField, roleSlider = "Text", "Button", "CheckBox", "Edit", "Slider"

func accessibility(w *mygo.Window) (nodes []accessNode, ok bool) {
	mygo.RunOnMain(func() {
		var list []win.TestAccessNode
		list, ok = win.TestAccessibility(w.NativeHandle())
		for _, n := range list {
			nodes = append(nodes, accessNode{n.Role, n.Label, n.Value})
		}
	})
	return nodes, ok
}

func accessPerform(w *mygo.Window, label, action, value string) (ok bool) {
	mygo.RunOnMain(func() { ok = win.TestAccessibilityPerform(w.NativeHandle(), label, action, value) })
	return ok
}

// Typing into native UI is only automated on macOS.
func clickAndType(*mygo.Window, float64, float64, string) bool { return false }
func compose(*mygo.Window, string, int, bool) bool             { return false }

// Only Linux draws native UI in a GtkGLArea.
func glSurface(*mygo.Window) (string, []byte, int, int, bool) { return "", nil, 0, 0, false }

// rightClick clicks (x, y) in a window showing native UI with the
// secondary button, with the messages a mouse sends.
func rightClick(w *mygo.Window, x, y float64) (ok bool) {
	mygo.RunOnMain(func() { ok = win.TestRightClickSurface(w.NativeHandle(), x, y) })
	return ok
}

// popupMenus returns the labels of the items of the context menus shown.
// The modal loop of a menu runs the main thread's work.
func popupMenus() (menus [][]string, supported bool) {
	mygo.RunOnMain(func() { menus = win.TestPopups() })
	return menus, true
}

func choosePopupItem(label string) (ok bool) {
	mygo.RunOnMain(func() { ok = win.TestChoosePopupItem(label) })
	return ok
}
