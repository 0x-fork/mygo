//go:build windows && (amd64 || arm64)

package windows

import (
	"fmt"
	"unsafe"

	"github.com/egoist/mygo/internal/accelerator"
)

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

// TestMenuBarShown reports whether a window shows its menu bar.
func TestMenuBarShown(hwnd uintptr) bool {
	bar, _, _ := user32.NewProc("GetMenu").Call(hwnd)
	return bar != 0
}

// TestEnterMenuBar takes the keyboard to a window's menu bar, as Alt and
// F10 do, and returns once it leaves the menus. It reports false when the
// window cannot come to the front, which the keyboard needs.
func TestEnterMenuBar(hwnd uintptr) bool {
	procSetForegroundWindow.Call(hwnd)
	if fg, _, _ := procGetForegroundWindow.Call(); fg != hwnd {
		return false
	}
	procSendMessageW.Call(hwnd, wmSysCommand, scKeyMenu, 0)
	return true
}

// TestEndMenu leaves the menus, as Escape does.
func TestEndMenu() { user32.NewProc("EndMenu").Call() }

// TestActivateAccelerator runs the menu item of a window's shortcut, as its
// keys do in the page, and reports whether there is one.
func TestActivateAccelerator(hwnd uintptr, acc string) bool {
	w := theBackend.windows[hwnd]
	a, err := accelerator.Parse(acc, "windows")
	if w == nil || err != nil {
		return false
	}
	vk, ok := virtualKey(a.Key)
	if !ok {
		return false
	}
	cmd, ok := w.accels[accelKey{vk: vk, mods: a.Modifiers}]
	if ok {
		theBackend.menuCommand(cmd, w)
	}
	return ok
}

// TestSetDroppedFiles makes paths the files of the next drop on a window's
// page, as if they had been dragged there.
func TestSetDroppedFiles(hwnd uintptr, paths []string) {
	if w := theBackend.windows[hwnd]; w != nil {
		w.dropped = paths
	}
}

// TestPressKeys presses virtual keys together and releases them, like a
// keyboard would.
func TestPressKeys(keys ...byte) {
	proc := user32.NewProc("keybd_event")
	const keyUp = 0x2
	for _, k := range keys {
		proc.Call(uintptr(k), 0, 0, 0)
	}
	for i := len(keys) - 1; i >= 0; i-- {
		proc.Call(uintptr(keys[i]), 0, keyUp, 0)
	}
}

// TestCaptionButtons returns the window controls of a window with a hidden
// title bar, left to right, named after what WM_NCHITTEST answers over
// their middles, which Windows' snap layouts rely on.
func TestCaptionButtons(hwnd uintptr) []string {
	w := theBackend.windows[hwnd]
	if w == nil || w.caption == nil {
		return nil
	}
	var names []string
	for i := range w.caption.shown {
		hit, _, _ := procSendMessageW.Call(w.caption.buttons, wmNCHitTest, 0, captionButtonPoint(w.caption, i))
		switch hit {
		case htMinButton:
			names = append(names, "minimize")
		case htMaxButton:
			names = append(names, "maximize")
		case htClose:
			names = append(names, "close")
		default:
			names = append(names, fmt.Sprint(hit))
		}
	}
	return names
}

// TestPressCaptionButton clicks a window control of a window with a hidden
// title bar: "minimize", "maximize" or "close".
func TestPressCaptionButton(hwnd uintptr, name string) bool {
	w := theBackend.windows[hwnd]
	if w == nil || w.caption == nil {
		return false
	}
	for i, b := range w.caption.shown {
		if [...]string{"minimize", "maximize", "close"}[b] != name {
			continue
		}
		lp := captionButtonPoint(w.caption, i)
		procSendMessageW.Call(w.caption.buttons, wmNCLButtonDown, b.hitTest(), lp)
		procSendMessageW.Call(w.caption.buttons, wmNCLButtonUp, b.hitTest(), lp)
		return true
	}
	return false
}

// captionButtonPoint is the screen position of the middle of the i-th
// button, as the lParam of mouse messages.
func captionButtonPoint(c *captionBar, i int) uintptr {
	var r rect
	procGetWindowRect.Call(c.buttons, uintptr(unsafe.Pointer(&r)))
	bw := (r.Right - r.Left) / int32(len(c.shown))
	x, y := r.Left+int32(i)*bw+bw/2, r.Top+(r.Bottom-r.Top)/2
	return uintptr(uint16(x)) | uintptr(uint16(y))<<16
}
