//go:build darwin

package darwin

import (
	"runtime"
	"structs"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/internal/platform"
)

// The surface of a window that shows content MyGo draws itself is a
// layer-backed view in place of the web view, inside the same content view,
// so that vibrancy, the traffic lights and a hidden title bar behave as
// with a page. It turns AppKit's events into surface events, is the text
// input client of input methods, and paces frames with a display link.

// nsRange is NSRange, for the text input methods.
type nsRange struct {
	_                structs.HostLayout
	Location, Length uint
}

// cgRect is an NSRect returned by a method implemented in Go.
type cgRect struct {
	_          structs.HostLayout
	X, Y, W, H float64
}

const nsNotFound = uint(1<<63 - 1)

var (
	surfaceOnce        sync.Once
	msgConvertRectView func(obj id, sel objc.SEL, r NSRect, view id) NSRect
	msgInitTracking    func(obj id, sel objc.SEL, r NSRect, options uint, owner, info id) id
	msgTimer           func(cls id, sel objc.SEL, interval float64, target id, selector objc.SEL, info id, repeats bool) id
	cgColorSpaceSRGB   uintptr
	cgImageCreate      uintptr
	cgImageRelease     uintptr
	cgProviderCreate   uintptr
	cgProviderRelease  uintptr
	cfDataCreate       uintptr
)

func loadSurface() {
	surfaceOnce.Do(func() {
		stret := msgSendAddr
		if runtime.GOARCH == "amd64" {
			stret = mustDlsym(libObjC, "objc_msgSend_stret")
		}
		purego.RegisterFunc(&msgConvertRectView, stret)
		purego.RegisterFunc(&msgInitTracking, msgSendAddr)
		purego.RegisterFunc(&msgTimer, msgSendAddr)
		cgImageCreate = mustDlsym(libCG, "CGImageCreate")
		cgImageRelease = mustDlsym(libCG, "CGImageRelease")
		cgProviderCreate = mustDlsym(libCG, "CGDataProviderCreateWithCFData")
		cgProviderRelease = mustDlsym(libCG, "CGDataProviderRelease")
		cfDataCreate = mustDlsym(libCF, "CFDataCreate")
		p := mustDlsym(libCG, "kCGColorSpaceSRGB")
		name := **(**uintptr)(unsafe.Pointer(&p))
		cgColorSpaceSRGB, _, _ = purego.SyscallN(mustDlsym(libCG, "CGColorSpaceCreateWithName"), name)
	})
}

type surface struct {
	w        *window
	view     id
	tracking id
	link     id // CADisplayLink (macOS 14), nil before
	// The display link runs while frames follow each other (linkRunning),
	// and due is whether its next tick draws one. Without a display link,
	// a timer paces frames at the display's rate: timing is true while
	// one is due. lastFrame is when the last frame began.
	linkRunning bool
	due         bool
	timing      bool
	lastFrame   time.Time
	cursor      platform.Cursor
	inside      bool
	ctrlClick   bool // the primary button is down for a Control-click

	// input is the state of the text input with the keyboard; marked is
	// the input method's composition and markedSel its selection in it.
	input     platform.TextInputState
	marked    string
	markedSel nsRange
	keyDown   bool // in interpretKeyEvents: text typed with the key

	// Accessibility: whether the content describes its frames, the last
	// tree, the elements of its nodes and those at the top (an NSArray).
	accessOn bool
	access   platform.AccessTree
	elements map[uint64]*accessElement
	topLevel id
	updating bool
}

func (w *window) createSurface(content NSRect) {
	loadSurface()
	s := &surface{w: w}
	s.view = msgInitRect(send(class("MyGoSurfaceView"), "alloc"), sel("initWithFrame:"), NSRect{Size: content.Size})
	send(s.view, "setWantsLayer:", 1)
	send(s.view, "setLayerContentsRedrawPolicy:", 2) // on setNeedsDisplay
	send(s.view, "setAutoresizingMask:", nsViewWidthHeightSizable)
	layer := send(s.view, "layer")
	send(layer, "setContentsGravity:", uintptr(nsString("resize")))
	// Tracking for hover, inside the visible part of the view.
	const options = 0x01 | 0x02 | 0x04 | 0x40 | 0x200 // entered/exited, moved, cursor update, active app, visible rect
	s.tracking = msgInitTracking(send(class("NSTrackingArea"), "alloc"), sel("initWithRect:options:owner:userInfo:"), NSRect{}, options, s.view, 0)
	send(s.view, "addTrackingArea:", uintptr(s.tracking))
	if respondsTo(s.view, "displayLinkWithTarget:selector:") {
		s.link = retain(send(s.view, "displayLinkWithTarget:selector:", uintptr(s.view), uintptr(sel("mygoTick:"))))
		send(s.link, "setPaused:", 1)
		send(s.link, "addToRunLoop:forMode:", uintptr(send(class("NSRunLoop"), "mainRunLoop")), kCFRunLoopCommonModes)
	}
	// Files dragged from other apps.
	send(s.view, "registerForDraggedTypes:", uintptr(nsArray(nsString("public.file-url"))))
	w.surface = s
	w.b.bySurface[s.view] = s
}

func (s *surface) destroy() {
	if s.link != 0 {
		send(s.link, "invalidate")
		release(s.link)
		s.link = 0
	}
	s.destroyAccess()
	send(s.view, "removeTrackingArea:", uintptr(s.tracking))
	release(s.tracking)
	delete(s.w.b.bySurface, s.view)
	release(s.view)
}

// Surface returns the surface of a window created with
// WindowOptions.Surface.
func (w *window) Surface() platform.Surface {
	if w.surface == nil {
		return nil
	}
	return w.surface
}

func (s *surface) Native() platform.SurfaceNative {
	return platform.SurfaceNative{View: uintptr(s.view), Layer: uintptr(send(s.view, "layer"))}
}

func (s *surface) scale() float64 {
	if f := msgFloat(s.w.win, sel("backingScaleFactor")); f > 0 {
		return f
	}
	return 1
}

func (s *surface) Size() (float64, float64, float64) {
	b := msgRect(s.view, sel("bounds"))
	return b.Size.Width, b.Size.Height, s.scale()
}

func (s *surface) RequestFrame() {
	if s.w.closed {
		return
	}
	if s.link != 0 {
		switch {
		case s.linkRunning:
			s.due = true
		case time.Since(s.lastFrame) > 2*s.refresh() && s.visible():
			// After a pause, the frame draws at once, at the end of this
			// pass of the run loop, as AppKit's views do. A window out of
			// sight waits for the link, which ticks once it shows: AppKit
			// draws the views of hidden windows too.
			send(s.view, "setNeedsDisplay:", 1)
		default:
			// Frames that follow each other draw at the display's
			// refreshes; the link keeps running while they come, rather
			// than pausing after each, which costs a call to the window
			// server.
			s.due, s.linkRunning = true, true
			send(s.link, "setPaused:", 0)
		}
		return
	}
	if s.timing {
		return
	}
	// One refresh after the last frame, or at once after a pause.
	delay := s.refresh() - time.Since(s.lastFrame)
	if delay <= 0 {
		send(s.view, "setNeedsDisplay:", 1)
		return
	}
	s.timing = true
	withPool(func() {
		t := msgTimer(class("NSTimer"), sel("timerWithTimeInterval:target:selector:userInfo:repeats:"), delay.Seconds(), s.view, sel("mygoTimer:"), 0, false)
		send(send(class("NSRunLoop"), "mainRunLoop"), "addTimer:forMode:", uintptr(t), kCFRunLoopCommonModes)
	})
}

// visible reports whether some of the window shows on screen.
func (s *surface) visible() bool {
	const occlusionStateVisible = 1 << 1
	return sendInt(s.w.win, "occlusionState")&occlusionStateVisible != 0
}

// refresh returns the time between two refreshes of the window's display.
func (s *surface) refresh() time.Duration {
	fps := 60
	if screen := send(s.w.win, "screen"); screen != 0 && respondsTo(screen, "maximumFramesPerSecond") {
		fps = max(sendInt(screen, "maximumFramesPerSecond"), 30)
	}
	return time.Second / time.Duration(fps)
}

func (s *surface) PresentPixels(pix []byte, stride, width, height int) {
	if len(pix) < stride*height || width == 0 || height == 0 {
		return
	}
	data, _, _ := purego.SyscallN(cfDataCreate, 0, uintptr(unsafe.Pointer(&pix[0])), uintptr(stride*height))
	provider, _, _ := purego.SyscallN(cgProviderCreate, data)
	const bitmapInfo = 2 | 2<<12 // premultiplied alpha first, 32-bit little endian: BGRA
	img, _, _ := purego.SyscallN(cgImageCreate, uintptr(width), uintptr(height), 8, 32, uintptr(stride), cgColorSpaceSRGB, bitmapInfo, provider, 0, 0, 0)
	layer := send(s.view, "layer")
	msgSetFloat(layer, sel("setContentsScale:"), s.scale())
	send(layer, "setContents:", img)
	purego.SyscallN(cgImageRelease, img)
	purego.SyscallN(cgProviderRelease, provider)
	cfRelease(data)
}

var cursorSelectors = map[platform.Cursor]string{
	platform.CursorDefault:    "arrowCursor",
	platform.CursorPointer:    "pointingHandCursor",
	platform.CursorText:       "IBeamCursor",
	platform.CursorMove:       "openHandCursor",
	platform.CursorResizeEW:   "resizeLeftRightCursor",
	platform.CursorResizeNS:   "resizeUpDownCursor",
	platform.CursorNotAllowed: "operationNotAllowedCursor",
	platform.CursorCrosshair:  "crosshairCursor",
	platform.CursorGrab:       "openHandCursor",
	platform.CursorGrabbing:   "closedHandCursor",
}

func (s *surface) applyCursor() {
	send(nsCursor(s.cursor), "set")
}

// nsCursor returns the NSCursor of c. The diagonal resize cursors are the
// frame resize cursors of macOS 15, else AppKit's own older ones.
func nsCursor(c platform.Cursor) id {
	cls := class("NSCursor")
	if c == platform.CursorResizeNWSE || c == platform.CursorResizeNESW {
		corner, older := uintptr(1|2), "_windowResizeNorthWestSouthEastCursor" // top left
		if c == platform.CursorResizeNESW {
			corner, older = 1|8, "_windowResizeNorthEastSouthWestCursor" // top right
		}
		if respondsTo(cls, "frameResizeCursorFromPosition:inDirections:") {
			if cur := send(cls, "frameResizeCursorFromPosition:inDirections:", corner, 1|2); cur != 0 { // inward and outward
				return cur
			}
		}
		if respondsTo(cls, older) {
			if cur := send(cls, older); cur != 0 {
				return cur
			}
		}
	}
	name, ok := cursorSelectors[c]
	if !ok {
		name = "arrowCursor"
	}
	return send(cls, name)
}

func (s *surface) SetCursor(c platform.Cursor) {
	s.cursor = c
	if s.inside {
		s.applyCursor()
	}
}

func (s *surface) SetTextInput(t platform.TextInputState) {
	if s.input.Active && !t.Active && s.marked != "" {
		s.marked = ""
		send(send(s.view, "inputContext"), "discardMarkedText")
	}
	moved := t.Active != s.input.Active || t.Caret != s.input.Caret
	s.input = t
	if t.Active && moved {
		send(send(s.view, "inputContext"), "invalidateCharacterCoordinates")
	}
}

// surfaceKeyChanged tells the content of a window that became or ceased
// to be the key window that its surface gained or lost the keyboard, as
// focus events do on the other platforms: AppKit tells views only when
// the first responder changes.
func (w *window) surfaceKeyChanged(key bool) {
	s := w.surface
	if s == nil || send(w.win, "firstResponder") != s.view {
		return
	}
	kind := platform.SurfaceBlur
	if key {
		kind = platform.SurfaceFocus
	}
	s.send(platform.SurfaceEvent{Kind: kind})
}

func (s *surface) send(ev platform.SurfaceEvent) bool {
	if s.w.closed {
		return false
	}
	return s.w.h.SurfaceEvent(ev)
}

func eventMods(ev id) platform.Modifiers {
	flags := uint(send(ev, "modifierFlags"))
	var m platform.Modifiers
	if flags&(1<<17) != 0 {
		m |= platform.ModShift
	}
	if flags&(1<<18) != 0 {
		m |= platform.ModCtrl
	}
	if flags&(1<<19) != 0 {
		m |= platform.ModAlt
	}
	if flags&(1<<20) != 0 {
		m |= platform.ModSuper
	}
	return m
}

// location returns where a mouse event happened, in the view's flipped
// coordinates.
func (s *surface) location(ev id) (float64, float64) {
	p := msgPointFromView(s.view, sel("convertPoint:fromView:"), msgPoint(ev, sel("locationInWindow")), 0)
	return p.X, p.Y
}

func (s *surface) mouse(kind platform.SurfaceEventKind, ev id, button int) {
	x, y := s.location(ev)
	s.send(platform.SurfaceEvent{Kind: kind, X: x, Y: y, Button: button, Clicks: sendInt(ev, "clickCount"), Mods: eventMods(ev)})
}

// macKeys maps virtual key codes that do not type a character.
var macKeys = map[uint16]platform.Key{
	0x24: platform.KeyEnter, 0x4C: platform.KeyEnter, 0x30: platform.KeyTab, 0x31: platform.KeySpace,
	0x33: platform.KeyBackspace, 0x35: platform.KeyEscape, 0x75: platform.KeyDelete, 0x72: platform.KeyInsert,
	0x73: platform.KeyHome, 0x77: platform.KeyEnd, 0x74: platform.KeyPageUp, 0x79: platform.KeyPageDown,
	0x7B: platform.KeyLeft, 0x7C: platform.KeyRight, 0x7D: platform.KeyDown, 0x7E: platform.KeyUp,
	0x7A: platform.KeyF1, 0x78: platform.KeyF2, 0x63: platform.KeyF3, 0x76: platform.KeyF4,
	0x60: platform.KeyF5, 0x61: platform.KeyF6, 0x62: platform.KeyF7, 0x64: platform.KeyF8,
	0x65: platform.KeyF9, 0x6D: platform.KeyF10, 0x67: platform.KeyF11, 0x6F: platform.KeyF12,
	0x6E: platform.KeyContextMenu,
}

func eventKey(ev id) platform.Key {
	code := uint16(send(ev, "keyCode"))
	if k, ok := macKeys[code]; ok {
		return k
	}
	chars := goString(send(ev, "charactersIgnoringModifiers"))
	for _, r := range chars {
		return platform.KeyForRune(r)
	}
	return platform.KeyUnknown
}

// stringOf returns the text of an NSString or NSAttributedString.
func stringOf(obj id) string {
	if obj == 0 {
		return ""
	}
	if respondsTo(obj, "string") {
		obj = send(obj, "string")
	}
	return goString(obj)
}

// utf16Len returns the length of s in UTF-16 code units, as NSRanges count.
func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// runesBefore converts a UTF-16 offset in s into a rune offset.
func runesBefore(s string, units int) int {
	u := utf16.Encode([]rune(s))
	units = max(0, min(units, len(u)))
	return len(utf16.Decode(u[:units]))
}

func (b *Backend) surfaceOf(view id) *surface {
	s := b.bySurface[view]
	if s == nil || s.w.closed {
		return nil
	}
	return s
}

func registerSurfaceClass() {
	b := func() *Backend { return theBackend }
	mouse := func(kind platform.SurfaceEventKind, button int) func(id, objc.SEL, id) {
		return func(self id, _ objc.SEL, ev id) {
			s := b().surfaceOf(self)
			if s == nil {
				return
			}
			btn := button
			switch {
			case button == 2:
				btn = min(sendInt(ev, "buttonNumber"), 2)
			case button == 0 && kind == platform.PointerDown:
				// A Control-click is a secondary click, until its release.
				s.ctrlClick = eventMods(ev)&platform.ModCtrl != 0
			}
			if button == 0 && s.ctrlClick {
				btn = 1
			}
			if kind == platform.PointerDown {
				if btn == 0 {
					release(s.w.lastMouseDown)
					s.w.lastMouseDown = retain(ev)
				}
				send(s.w.win, "makeFirstResponder:", uintptr(self))
			}
			s.mouse(kind, ev, btn)
			if kind == platform.PointerUp && button == 0 {
				s.ctrlClick = false
			}
		}
	}
	command := func(name string) objc.MethodDef {
		return method(name+":", func(self id, _ objc.SEL, sender id) {
			if s := b().surfaceOf(self); s != nil {
				s.send(platform.SurfaceEvent{Kind: platform.SurfaceCommand, Text: name})
			}
		})
	}
	dragged := func(self id, _ objc.SEL, info id) uint {
		s := b().surfaceOf(self)
		if s == nil {
			return 0
		}
		x, y := s.dragPoint(info)
		if s.send(platform.SurfaceEvent{Kind: platform.FileDragOver, X: x, Y: y}) {
			return 1 // NSDragOperationCopy
		}
		return 0
	}
	methods := []objc.MethodDef{
		// Files dragged from other apps.
		method("draggingEntered:", dragged),
		method("draggingUpdated:", dragged),
		method("draggingExited:", func(self id, _ objc.SEL, info id) {
			if s := b().surfaceOf(self); s != nil {
				s.send(platform.SurfaceEvent{Kind: platform.FileDragLeave})
			}
		}),
		method("prepareForDragOperation:", func(self id, _ objc.SEL, info id) bool { return true }),
		method("performDragOperation:", func(self id, _ objc.SEL, info id) bool {
			s := b().surfaceOf(self)
			if s == nil {
				return false
			}
			x, y := s.dragPoint(info)
			return s.send(platform.SurfaceEvent{Kind: platform.FileDrop, X: x, Y: y, Files: draggedFiles(info)})
		}),
	}
	classDef("MyGoSurfaceView", "NSView", []string{"NSTextInputClient"}, append(append(methods, accessViewMethods()...), []objc.MethodDef{
		method("isFlipped", func(self id, _ objc.SEL) bool { return true }),
		method("acceptsFirstResponder", func(self id, _ objc.SEL) bool { return true }),
		method("acceptsFirstMouse:", func(self id, _ objc.SEL, ev id) bool { return true }),
		method("mouseDownCanMoveWindow", func(self id, _ objc.SEL) bool { return false }),
		method("wantsUpdateLayer", func(self id, _ objc.SEL) bool { return true }),
		method("updateLayer", func(self id, _ objc.SEL) {
			if s := b().surfaceOf(self); s != nil {
				s.lastFrame = time.Now()
				s.send(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
			}
		}),
		method("mygoTick:", func(self id, _ objc.SEL, link id) {
			s := b().surfaceOf(self)
			if s == nil || !s.due {
				// No frame was asked for since the last refresh.
				send(link, "setPaused:", 1)
				if s != nil {
					s.linkRunning = false
				}
				return
			}
			s.due = false
			send(self, "setNeedsDisplay:", 1)
		}),
		method("mygoTimer:", func(self id, _ objc.SEL, timer id) {
			// A closed window's view lives until its timer fires.
			if s := b().surfaceOf(self); s != nil {
				s.timing = false
				send(self, "setNeedsDisplay:", 1)
			}
		}),
		method("setFrameSize:", func(self id, cmd objc.SEL, size NSSize) {
			objc.ID(self).SendSuper(cmd, size)
			if s := b().surfaceOf(self); s != nil {
				s.send(platform.SurfaceEvent{Kind: platform.SurfaceResize})
				send(self, "setNeedsDisplay:", 1)
			}
		}),
		method("viewDidChangeBackingProperties", func(self id, _ objc.SEL) {
			if s := b().surfaceOf(self); s != nil {
				s.send(platform.SurfaceEvent{Kind: platform.SurfaceResize})
				send(self, "setNeedsDisplay:", 1)
			}
		}),
		method("becomeFirstResponder", func(self id, _ objc.SEL) bool {
			if s := b().surfaceOf(self); s != nil {
				s.send(platform.SurfaceEvent{Kind: platform.SurfaceFocus})
			}
			return true
		}),
		method("resignFirstResponder", func(self id, _ objc.SEL) bool {
			if s := b().surfaceOf(self); s != nil {
				s.send(platform.SurfaceEvent{Kind: platform.SurfaceBlur})
			}
			return true
		}),
		method("mouseDown:", mouse(platform.PointerDown, 0)),
		method("mouseUp:", mouse(platform.PointerUp, 0)),
		method("mouseDragged:", mouse(platform.PointerMove, 0)),
		method("rightMouseDown:", mouse(platform.PointerDown, 1)),
		method("rightMouseUp:", mouse(platform.PointerUp, 1)),
		method("rightMouseDragged:", mouse(platform.PointerMove, 1)),
		method("otherMouseDown:", mouse(platform.PointerDown, 2)),
		method("otherMouseUp:", mouse(platform.PointerUp, 2)),
		method("otherMouseDragged:", mouse(platform.PointerMove, 2)),
		method("mouseMoved:", mouse(platform.PointerMove, 0)),
		method("mouseEntered:", func(self id, _ objc.SEL, ev id) {
			if s := b().surfaceOf(self); s != nil {
				s.inside = true
				s.applyCursor()
			}
		}),
		method("mouseExited:", func(self id, _ objc.SEL, ev id) {
			if s := b().surfaceOf(self); s != nil {
				s.inside = false
				s.send(platform.SurfaceEvent{Kind: platform.PointerLeave})
			}
		}),
		method("cursorUpdate:", func(self id, _ objc.SEL, ev id) {
			if s := b().surfaceOf(self); s != nil {
				s.applyCursor()
			}
		}),
		method("scrollWheel:", func(self id, _ objc.SEL, ev id) {
			s := b().surfaceOf(self)
			if s == nil {
				return
			}
			x, y := s.location(ev)
			dx, dy := msgFloat(ev, sel("scrollingDeltaX")), msgFloat(ev, sel("scrollingDeltaY"))
			precise := sendBool(ev, "hasPreciseScrollingDeltas")
			if !precise {
				// A mouse wheel scrolls by lines: 40 DIPs, as in browsers.
				dx, dy = dx*40, dy*40
			}
			s.send(platform.SurfaceEvent{Kind: platform.PointerScroll, X: x, Y: y, DX: -dx, DY: -dy, Precise: precise, Mods: eventMods(ev)})
		}),
		method("keyDown:", func(self id, _ objc.SEL, ev id) {
			s := b().surfaceOf(self)
			if s == nil {
				return
			}
			mods := eventMods(ev)
			s.send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: eventKey(ev), Mods: mods, Repeat: sendBool(ev, "isARepeat")})
			// Input methods see the key while a text input has the focus;
			// they answer with insertText: or setMarkedText:.
			if s.input.Active && mods&(platform.ModSuper|platform.ModCtrl) == 0 {
				s.keyDown = true
				send(self, "interpretKeyEvents:", uintptr(nsArray(ev)))
				s.keyDown = false
			}
		}),
		method("keyUp:", func(self id, _ objc.SEL, ev id) {
			if s := b().surfaceOf(self); s != nil {
				s.send(platform.SurfaceEvent{Kind: platform.KeyReleased, Key: eventKey(ev), Mods: eventMods(ev)})
			}
		}),
		command("copy"), command("cut"), command("paste"), command("selectAll"), command("undo"), command("redo"), command("delete"),
		method("pasteAsPlainText:", func(self id, _ objc.SEL, sender id) {
			if s := b().surfaceOf(self); s != nil {
				s.send(platform.SurfaceEvent{Kind: platform.SurfaceCommand, Text: "paste"})
			}
		}),
		method("validateMenuItem:", func(self id, _ objc.SEL, item id) bool { return true }),

		// NSTextInputClient
		method("hasMarkedText", func(self id, _ objc.SEL) bool {
			s := b().surfaceOf(self)
			return s != nil && s.marked != ""
		}),
		method("markedRange", func(self id, _ objc.SEL) nsRange {
			if s := b().surfaceOf(self); s != nil {
				return s.markedRange()
			}
			return nsRange{Location: nsNotFound}
		}),
		method("selectedRange", func(self id, _ objc.SEL) nsRange {
			if s := b().surfaceOf(self); s != nil {
				return s.selectedRange()
			}
			return nsRange{Location: nsNotFound}
		}),
		method("setMarkedText:selectedRange:replacementRange:", func(self id, _ objc.SEL, text id, selected, replacement nsRange) {
			if s := b().surfaceOf(self); s != nil {
				s.setMarkedText(stringOf(text), selected, replacement)
			}
		}),
		method("unmarkText", func(self id, _ objc.SEL) {
			if s := b().surfaceOf(self); s != nil && s.marked != "" {
				s.marked = ""
				s.send(platform.SurfaceEvent{Kind: platform.TextComposition})
			}
		}),
		method("validAttributesForMarkedText", func(self id, _ objc.SEL) id { return nsArray() }),
		method("attributedSubstringForProposedRange:actualRange:", func(self id, _ objc.SEL, r nsRange, actual *nsRange) id {
			if s := b().surfaceOf(self); s != nil {
				return s.substring(r, actual)
			}
			return 0
		}),
		method("insertText:replacementRange:", func(self id, _ objc.SEL, text id, replacement nsRange) {
			if s := b().surfaceOf(self); s != nil {
				s.insertText(stringOf(text), replacement)
			}
		}),
		method("characterIndexForPoint:", func(self id, _ objc.SEL, p NSPoint) uint { return nsNotFound }),
		method("firstRectForCharacterRange:actualRange:", func(self id, _ objc.SEL, r nsRange, actual *nsRange) cgRect {
			s := b().surfaceOf(self)
			if s == nil {
				return cgRect{}
			}
			c := s.input.Caret
			inWindow := msgConvertRectView(self, sel("convertRect:toView:"), NSRect{Origin: NSPoint{c.X, c.Y}, Size: NSSize{max(c.W, 1), c.H}}, 0)
			screen := msgRectToRect(s.w.win, sel("convertRectToScreen:"), inWindow)
			return cgRect{X: screen.Origin.X, Y: screen.Origin.Y, W: screen.Size.Width, H: screen.Size.Height}
		}),
		method("doCommandBySelector:", func(self id, _ objc.SEL, cmd objc.SEL) {}),
	}...))
}

// dragPoint returns where a drag is, in the view's coordinates.
func (s *surface) dragPoint(info id) (float64, float64) {
	p := msgPointFromView(s.view, sel("convertPoint:fromView:"), msgPoint(info, sel("draggingLocation")), 0)
	return p.X, p.Y
}
