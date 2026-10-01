//go:build linux && (amd64 || arm64)

package e2e

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/linux"
)

func activateMenu(w *mygo.Window, path ...string) (err error) {
	mygo.RunOnMain(func() { err = linux.TestActivateMenuItem(w.NativeHandle(), path...) })
	return err
}

// Key presses and dialogs need an input device under Xvfb; not covered.
func pressShortcut(string, bool) (bool, bool) { return false, false }

func endSheet(*mygo.Window) (bool, bool) { return false, false }

func click(*mygo.Window, float64, float64) bool { return false }

func webViewAttached(*mygo.Window) (bool, bool) { return false, false }

// WebKitGTK docks the inspector inside the web view.
func dockDevTools(*mygo.Window) bool { return false }

func dockedDevToolsPlace(*mygo.Window) string { return "" }

func dismissPopups() (n int, supported bool) {
	mygo.RunOnMain(func() { n = linux.TestDismissPopups() })
	return n, true
}

func setDroppedFiles(w *mygo.Window, paths []string) bool {
	mygo.RunOnMain(func() { linux.TestSetDroppedFiles(w.NativeHandle(), paths) })
	return true
}

// The progress bar is shown by the shell, out of the app's reach.
func dockTileImage() ([]byte, bool) { return nil, false }

func defaultURLHandler(scheme string) (id string, supported bool) {
	mygo.RunOnMain(func() { id = linux.TestDefaultURLHandler(scheme) })
	return id, true
}

func dockMenu(int) ([]string, bool) { return nil, false }

// pressCtrlShiftK presses Ctrl+Shift+K like a keyboard.
func pressCtrlShiftK() (ok bool) {
	mygo.RunOnMain(func() { ok = linux.TestPressKeys("Control_L", "Shift_L", "k") })
	return ok
}

// usePortalShortcuts makes global shortcuts bind through the XDG desktop
// portal, as on Wayland, when the desktop offers it.
func usePortalShortcuts() (restore func(), ok bool) {
	mygo.RunOnMain(func() { ok = linux.TestUsePortalShortcuts(true) })
	return func() { mygo.RunOnMain(func() { linux.TestUsePortalShortcuts(false) }) }, ok
}

// pressKeys presses keys, X keysym names such as "Control_L", together.
func pressKeys(keys ...string) (ok bool) {
	mygo.RunOnMain(func() { ok = linux.TestPressKeys(keys...) })
	return ok
}

// Only macOS windows have a toolbar.
func fullScreenHidesToolbar(*mygo.Window) (bool, bool) { return false, false }

// Only macOS windows have traffic lights.
func trafficLights(*mygo.Window) (float64, float64, bool) { return 0, 0, false }

// movePointer moves the pointer to a point of the screen, and pressButton
// presses or releases the first mouse button, like a mouse.
func movePointer(x, y int) (ok bool) {
	mygo.RunOnMain(func() { ok = linux.TestMovePointer(x, y) })
	return ok
}

func pressButton(press bool) (ok bool) {
	mygo.RunOnMain(func() { ok = linux.TestPressButton(press) })
	return ok
}

func resizeCursor(w *mygo.Window) (name string, supported bool) {
	mygo.RunOnMain(func() { name = linux.TestResizeCursor(w.NativeHandle()) })
	return name, true
}

func menuBarShown(w *mygo.Window) (shown, supported bool) {
	mygo.RunOnMain(func() { shown, _ = linux.TestMenuBarState(w.NativeHandle()) })
	return shown, true
}

func activateAccelerator(w *mygo.Window, accel string) (handled, supported bool) {
	mygo.RunOnMain(func() { handled = linux.TestActivateAccelerator(w.NativeHandle(), accel) })
	return handled, true
}

// enterMenuBar presses key, an X keysym name, on the window: the bar should
// show with its first menu open. Then it closes the menus, as Escape does.
func enterMenuBar(w *mygo.Window, key string) (during, after, supported bool) {
	if !w.IsFocused() || !pressKeys(key) {
		return false, false, false
	}
	state := func() (shown, open bool) {
		mygo.RunOnMain(func() { shown, open = linux.TestMenuBarState(w.NativeHandle()) })
		return shown, open
	}
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if shown, open := state(); shown && open {
			during = true
			break
		}
	}
	mygo.RunOnMain(func() { linux.TestCloseMenuBar(w.NativeHandle()) })
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if after, _ = state(); !after {
			break
		}
	}
	return during, after, true
}

// titleButtons returns GTK's title buttons over the page of a window with
// a hidden title bar.
func titleButtons(w *mygo.Window) (names []string, supported bool) {
	mygo.RunOnMain(func() { names = linux.TestTitleButtons(w.NativeHandle()) })
	return names, true
}

func pressTitleButton(w *mygo.Window, name string) (ok bool) {
	mygo.RunOnMain(func() { ok = linux.TestPressTitleButton(w.NativeHandle(), name) })
	return ok
}

func topNonClient(*mygo.Window) (int32, int32, bool) { return 0, 0, false }
