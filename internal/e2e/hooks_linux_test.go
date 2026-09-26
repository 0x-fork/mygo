//go:build linux

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
