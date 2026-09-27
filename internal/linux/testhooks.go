//go:build linux && (amd64 || arm64)

package linux

import "fmt"

// The functions in this file drive native UI the way a user would, for the
// GUI tests in internal/e2e. They must run on the main thread.

// TestActivateMenuItem activates an item of a window's menu bar, found by
// the labels along its path.
func TestActivateMenuItem(handle uintptr, path ...string) error {
	var w *window
	for _, x := range theBackend.windows {
		if x.win == handle {
			w = x
		}
	}
	if w == nil || w.menubar == 0 {
		return fmt.Errorf("mygo: window has no menu bar")
	}
	shell := w.menubar
	for i, label := range path {
		item := findMenuItem(shell, label)
		if item == 0 {
			return fmt.Errorf("mygo: no menu item %q", label)
		}
		if i == len(path)-1 {
			gtkMenuItemActivate(item)
			return nil
		}
		shell = gtkMenuItemGetSubmenu(item)
		if shell == 0 {
			return fmt.Errorf("mygo: menu %q has no submenu", label)
		}
	}
	return nil
}

func findMenuItem(shell ptr, label string) ptr {
	list := gtkContainerGetChildren(shell)
	defer gListFree(list)
	for node := list; node != 0; node = field[ptr](node, 8) {
		item := field[ptr](node, 0)
		if goStr(gtkMenuItemGetLabel(item)) == label {
			return item
		}
	}
	return 0
}

// TestDismissPopups closes the context menus being shown, as if the user
// pressed Escape, and reports how many there were.
func TestDismissPopups() int {
	for _, m := range theBackend.popups {
		gtkMenuShellDeactivate(m)
	}
	return len(theBackend.popups)
}

// TestSetDroppedFiles makes paths the files of the next drop on a window's
// page, as if they had been dragged there.
func TestSetDroppedFiles(handle uintptr, paths []string) {
	for _, w := range theBackend.windows {
		if w.win == handle {
			w.dropped = paths
		}
	}
}
