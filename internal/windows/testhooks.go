//go:build windows && (amd64 || arm64)

package windows

import "fmt"

// The functions in this file drive native UI the way a user would, for the
// GUI tests in internal/e2e. They must run on the main thread.

// TestActivateMenuItem activates an item of a window's menu bar, found by
// the labels along its path, e.g. TestActivateMenuItem(hwnd, "File", "New").
func TestActivateMenuItem(hwnd uintptr, path ...string) error {
	w := theBackend.windows[hwnd]
	if w == nil || w.menu == nil {
		return fmt.Errorf("mygo: the window has no menu bar")
	}
	items := w.menu.Items
	for i, label := range path {
		found := false
		for _, it := range items {
			if it.Label != label {
				continue
			}
			found = true
			if i == len(path)-1 {
				for cmd, e := range theBackend.menus.entries {
					if e.uid == it.ID && e.owner == w.owner {
						theBackend.menuCommand(cmd, w)
						return nil
					}
				}
				return fmt.Errorf("mygo: menu item %q is not a command", label)
			}
			if it.Submenu == nil {
				return fmt.Errorf("mygo: menu %q has no submenu", label)
			}
			items = it.Submenu.Items
			break
		}
		if !found {
			return fmt.Errorf("mygo: no menu item %q", label)
		}
	}
	return nil
}
