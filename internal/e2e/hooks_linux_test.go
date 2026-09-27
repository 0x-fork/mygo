//go:build linux && (amd64 || arm64)

package e2e

import (
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
