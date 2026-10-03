// Package surface connects the content of a window that MyGo draws itself
// (package ui) to the window (package mygo), without either importing the
// other's internals.
package surface

import "github.com/egoist/mygo/internal/platform"

// Conn is a window's side of the connection. Package mygo fills it before
// calling Content.AttachContent; the content sets the hooks it handles.
// Everything runs on the main thread, except Invalidate.
type Conn struct {
	Surface platform.Surface
	// Window is the *mygo.Window.
	Window any
	// Clipboard is the system clipboard.
	Clipboard platform.Clipboard
	// StartDrag moves the window with the pointer, for drag regions;
	// TitleBarDoubleClicked does what a double click on a title bar does.
	StartDrag             func()
	TitleBarDoubleClicked func()
	// IsDark reports the system's dark appearance.
	IsDark func() bool
	// UIFont returns the family of the desktop's interface font where the
	// system's text stack does not know it (Linux), else "".
	UIFont func() string
	// FontRendering returns how the desktop's settings say to rasterize
	// text where the system's text stack does not know them (Linux).
	FontRendering func() platform.FontRendering
	// TitleBar returns the room the window controls take in a window with
	// a hidden title bar, zero in other windows.
	TitleBar func() platform.TitleBar
	// OpenURL opens a link in the default browser.
	OpenURL func(url string)
	// Invalidate asks for a frame; it is safe from any goroutine.
	Invalidate func()
	// PopupMenu shows m as a context menu at (x, y) in the surface, in
	// DIPs, once the event being handled returns. chosen receives the ID of
	// the item chosen, if one is.
	PopupMenu func(m *platform.Menu, x, y float64, chosen func(id int))

	// Event receives the surface's events, and Focus and Blur of the
	// window. It reports whether the content takes dragged files, as
	// WindowHandler.SurfaceEvent does.
	Event func(ev platform.SurfaceEvent) bool
	// ThemeChanged is called when the system appearance changes, and
	// TitleBarChanged when TitleBar does.
	ThemeChanged    func()
	TitleBarChanged func()
	// Capture renders the content as it is now into premultiplied RGBA.
	Capture func() (width, height int, rgba []byte)
	// Detach is called once the window is closed.
	Detach func()
}

// Content is implemented by package ui.
type Content interface {
	AttachContent(conn *Conn)
}
