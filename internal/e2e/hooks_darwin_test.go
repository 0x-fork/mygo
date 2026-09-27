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

// Context menus track the mouse in a modal loop; not automated.
func dismissPopups() (int, bool) { return 0, false }

func setDroppedFiles(w *mygo.Window, paths []string) bool {
	mygo.RunOnMain(func() { darwin.TestSetDroppedFiles(w.NativeHandle(), paths) })
	return true
}
