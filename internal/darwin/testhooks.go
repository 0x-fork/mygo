//go:build darwin

package darwin

import (
	"fmt"
)

// The functions in this file drive native UI the way a user would, for the
// GUI tests in internal/e2e. They must run on the main thread.

// TestPerformMenuItem activates an item of the application menu, found by
// the titles along its path, e.g. TestPerformMenuItem("File", "New").
func TestPerformMenuItem(path ...string) error {
	var err error
	withPool(func() {
		menu := send(theBackend.app, "mainMenu")
		for i, title := range path {
			if menu == 0 {
				err = fmt.Errorf("mygo: menu %q has no submenu", path[i-1])
				return
			}
			index := sendInt(menu, "indexOfItemWithTitle:", uintptr(nsString(title)))
			if index < 0 {
				err = fmt.Errorf("mygo: no menu item %q", title)
				return
			}
			if i == len(path)-1 {
				send(menu, "performActionForItemAtIndex:", uintptr(index))
				return
			}
			menu = send(send(menu, "itemAtIndex:", uintptr(index)), "submenu")
		}
	})
	return err
}

// TestPerformKeyEquivalent sends a key press with the Command key (and
// shift when requested) to the application menu and reports whether an item
// handled it.
func TestPerformKeyEquivalent(key string, shift bool) bool {
	handled := false
	withPool(func() {
		flags := uint(1 << 20)
		if shift {
			flags |= 1 << 17
		}
		ev := msgKeyEvent(class("NSEvent"), sel("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"),
			10 /* NSEventTypeKeyDown */, NSPoint{}, flags, 0, 0, 0, nsString(key), nsString(key), false, 0)
		handled = sendBool(send(theBackend.app, "mainMenu"), "performKeyEquivalent:", uintptr(ev))
	})
	return handled
}

// TestEndSheet ends the sheet attached to a window as if its first button
// was clicked, and reports whether there was one.
func TestEndSheet(handle uintptr) bool {
	sheet := send(id(handle), "attachedSheet")
	if sheet == 0 {
		return false
	}
	send(id(handle), "endSheet:returnCode:", uintptr(sheet), nsAlertFirstButtonReturn)
	return true
}

// TestClick sends a left mouse down and up at a point of a window's content
// (top-left origin, in points), like a user click.
func TestClick(handle uintptr, x, y float64) {
	withPool(func() {
		win := id(handle)
		content := msgRect(send(win, "contentView"), sel("frame"))
		loc := NSPoint{x, content.Size.Height - y}
		number := sendInt(win, "windowNumber")
		for _, typ := range []uint{1, 2} { // NSEventTypeLeftMouseDown, LeftMouseUp
			ev := msgMouseEvent(class("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
				typ, loc, 0, 0, number, 0, 0, 1, 1)
			send(win, "sendEvent:", uintptr(ev))
		}
	})
}

// TestWebViewAttached reports whether a window's web view is in its view
// hierarchy.
func TestWebViewAttached(handle uintptr) bool {
	w := theBackend.byNSWindow[id(handle)]
	return w != nil && send(w.web, "window") == w.win
}

// TestSetDroppedFiles makes paths the files of the next drop on a window's
// page, as if they had been dragged there.
func TestSetDroppedFiles(handle uintptr, paths []string) {
	if w := theBackend.byNSWindow[id(handle)]; w != nil {
		w.dropped = paths
	}
}

// TestDockTileImage renders the content of the Dock tile offscreen, as the
// Dock does, to PNG; nil when the tile shows the plain application icon.
func TestDockTileImage() []byte {
	var data []byte
	withPool(func() {
		view := send(send(theBackend.app, "dockTile"), "contentView")
		if view == 0 {
			return
		}
		bounds := msgRect(view, sel("bounds"))
		rep := msgInitRect(view, sel("bitmapImageRepForCachingDisplayInRect:"), bounds)
		msgInitRectID(view, sel("cacheDisplayInRect:toBitmapImageRep:"), bounds, rep)
		data = goBytes(send(rep, "representationUsingType:properties:", 4, uintptr(send(class("NSDictionary"), "dictionary"))))
	})
	return data
}

// TestDockMenu returns the titles of the Dock menu the app delegate
// returns, and clicks the item at click unless it is -1.
func TestDockMenu(click int) []string {
	var titles []string
	withPool(func() {
		menu := send(send(theBackend.app, "delegate"), "applicationDockMenu:", uintptr(theBackend.app))
		if menu == 0 {
			return
		}
		for _, it := range arrayItems(send(menu, "itemArray")) {
			titles = append(titles, goString(send(it, "title")))
		}
		if click >= 0 {
			send(menu, "performActionForItemAtIndex:", uintptr(click))
		}
	})
	return titles
}
