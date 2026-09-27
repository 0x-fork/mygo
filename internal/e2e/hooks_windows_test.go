//go:build windows && (amd64 || arm64)

package e2e

import (
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

func dismissPopups() (int, bool) { return 0, false }

func setDroppedFiles(w *mygo.Window, paths []string) bool {
	mygo.RunOnMain(func() { win.TestSetDroppedFiles(w.NativeHandle(), paths) })
	return true
}

// The progress bar is shown by the shell, out of the app's reach.
func dockTileImage() ([]byte, bool) { return nil, false }

func defaultURLHandler(string) (string, bool) { return "", false }
