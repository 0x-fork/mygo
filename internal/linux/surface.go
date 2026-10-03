//go:build linux && (amd64 || arm64)

package linux

import (
	"log"
	"os"
	"sync"
	"unicode/utf8"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/gpu/gl"
	"github.com/egoist/mygo/internal/platform"
)

// The surface of a window that shows content MyGo draws itself is a
// GtkGLArea in place of the web view: frames are drawn with OpenGL in its
// render signal, into the framebuffer of the context GTK makes current,
// and GTK shows them. Without OpenGL, frames drawn in memory are painted
// with cairo in the draw signal, as on a GtkDrawingArea, which the surface
// is with MYGO_GPU=0. GTK's frame clock paces both; keys go through a
// GtkIMContext while a text input has the focus.

var (
	surfaceOnce                 sync.Once
	gtkDrawingAreaNew           func() ptr
	gtkWidgetSetCanFocus        func(w ptr, v bool)
	gtkWidgetAddEvents          func(w ptr, mask int32)
	gtkWidgetQueueDraw          func(w ptr)
	gtkWidgetGetAllocatedWidth  func(w ptr) int32
	gtkWidgetGetAllocatedHeight func(w ptr) int32
	gtkWidgetGetScaleFactor     func(w ptr) int32
	gtkWidgetGetDisplay         func(w ptr) ptr
	gtkWidgetHasFocus           func(w ptr) bool
	gtkIMMulticontextNew        func() ptr
	gtkIMContextSetClientWindow func(im, win ptr)
	gtkIMContextFilterKeypress  func(im, event ptr) bool
	gtkIMContextFocusIn         func(im ptr)
	gtkIMContextFocusOut        func(im ptr)
	gtkIMContextReset           func(im ptr)
	gtkIMContextSetCursorLoc    func(im ptr, r *gdkRectangle)
	gtkIMContextGetPreedit      func(im ptr, str *ptr, attrs *ptr, cursor *int32)
	gdkKeyvalToUnicode          func(keyval uint32) uint32
	cairoImageSurfaceForData    func(data *byte, format, width, height, stride int32) ptr
	cairoSurfaceSetDeviceScale  func(s ptr, x, y float64)
	cairoSetSourceSurface       func(cr, s ptr, x, y float64)
	cairoSetOperator            func(cr ptr, op int32)
	cairoPaint                  func(cr ptr)
	gtkGLAreaNew                func() ptr
	gtkGLAreaSetRequiredVersion func(a ptr, major, minor int32)
	gtkGLAreaSetHasAlpha        func(a ptr, alpha bool)
	gtkGLAreaGetError           func(a ptr) ptr
	gtkGLAreaSetError           func(a, gerr ptr)
	gdkWindowPeekChildren       func(w ptr) ptr
	gdkWindowGetUserData        func(w ptr, data *ptr)

	cbSurfaceDraw, cbSurfaceSize, cbSurfaceRealize, cbSurfaceButton ptr
	cbSurfaceMotion, cbSurfaceLeave, cbSurfaceScroll, cbSurfaceKey  ptr
	cbSurfaceFocusIn, cbSurfaceFocusOut, cbSurfaceScale, cbIMCommit ptr
	cbIMPreedit, cbIMPreeditEnd, cbSurfaceRender, cbAreaContext     ptr
	cbSurfaceUnrealize                                              ptr
	surfaceCursors                                                  = map[platform.Cursor]ptr{}
)

func loadSurface() {
	surfaceOnce.Do(func() {
		t, d, c := libGTK, libGDK, libCairo
		mustBind(t, &gtkDrawingAreaNew, "gtk_drawing_area_new")
		mustBind(t, &gtkWidgetSetCanFocus, "gtk_widget_set_can_focus")
		mustBind(t, &gtkWidgetAddEvents, "gtk_widget_add_events")
		mustBind(t, &gtkWidgetQueueDraw, "gtk_widget_queue_draw")
		mustBind(t, &gtkWidgetGetAllocatedWidth, "gtk_widget_get_allocated_width")
		mustBind(t, &gtkWidgetGetAllocatedHeight, "gtk_widget_get_allocated_height")
		mustBind(t, &gtkWidgetGetScaleFactor, "gtk_widget_get_scale_factor")
		mustBind(t, &gtkWidgetGetDisplay, "gtk_widget_get_display")
		mustBind(t, &gtkWidgetHasFocus, "gtk_widget_has_focus")
		mustBind(t, &gtkIMMulticontextNew, "gtk_im_multicontext_new")
		mustBind(t, &gtkIMContextSetClientWindow, "gtk_im_context_set_client_window")
		mustBind(t, &gtkIMContextFilterKeypress, "gtk_im_context_filter_keypress")
		mustBind(t, &gtkIMContextFocusIn, "gtk_im_context_focus_in")
		mustBind(t, &gtkIMContextFocusOut, "gtk_im_context_focus_out")
		mustBind(t, &gtkIMContextReset, "gtk_im_context_reset")
		mustBind(t, &gtkIMContextSetCursorLoc, "gtk_im_context_set_cursor_location")
		mustBind(t, &gtkIMContextGetPreedit, "gtk_im_context_get_preedit_string")
		mustBind(d, &gdkKeyvalToUnicode, "gdk_keyval_to_unicode")
		mustBind(c, &cairoImageSurfaceForData, "cairo_image_surface_create_for_data")
		mustBind(c, &cairoSurfaceSetDeviceScale, "cairo_surface_set_device_scale")
		mustBind(c, &cairoSetSourceSurface, "cairo_set_source_surface")
		mustBind(c, &cairoSetOperator, "cairo_set_operator")
		mustBind(c, &cairoPaint, "cairo_paint")
		mustBind(d, &gdkWindowPeekChildren, "gdk_window_peek_children")
		mustBind(d, &gdkWindowGetUserData, "gdk_window_get_user_data")
		// GtkGLArea came in GTK 3.16.
		if !bind(t, &gtkGLAreaNew, "gtk_gl_area_new") || !bind(t, &gtkGLAreaSetRequiredVersion, "gtk_gl_area_set_required_version") ||
			!bind(t, &gtkGLAreaSetHasAlpha, "gtk_gl_area_set_has_alpha") || !bind(t, &gtkGLAreaGetError, "gtk_gl_area_get_error") ||
			!bind(t, &gtkGLAreaSetError, "gtk_gl_area_set_error") {
			gtkGLAreaNew = nil
		}
	})
}

type surface struct {
	w      *window
	area   ptr // GtkGLArea, or GtkDrawingArea
	im     ptr // GtkIMContext
	cr     ptr // the cairo context of the draw signal in progress
	cursor platform.Cursor
	// gl tells that the area is a GtkGLArea, rendering that its render
	// signal is in progress, rendered that it ran.
	gl, rendering, rendered bool
	// present shows the frames the content draws in memory in a GtkGLArea,
	// as when its GL renderer fails; inMemory tells that it did, failed
	// that it failed too.
	present                 gl.Presenter
	inMemory, presentFailed bool

	textInput bool
	caret     platform.RectF
	input     platform.TextInputState
	// lastKey is a copy of the last key press, which a context menu the
	// key opens shows for.
	lastKey ptr
}

// GDK event masks of the drawing area.
const surfaceEvents = 1<<2 | 1<<8 | 1<<9 | 1<<10 | 1<<11 | 1<<12 | 1<<13 | 1<<14 | 1<<21 | 1<<23

func (w *window) createSurface() {
	loadSurface()
	data := ptr(w.id)
	s := &surface{w: w}
	s.im = gtkIMMulticontextNew()
	s.newArea(gtkGLAreaNew != nil && os.Getenv("MYGO_GPU") != "0" && gpuGL())
	connect(s.im, "commit", cbIMCommit, data)
	connect(s.im, "preedit-changed", cbIMPreedit, data)
	connect(s.im, "preedit-end", cbIMPreeditEnd, data)
	s.connectSystem(data)
	w.surface = s
}

// newArea creates the surface's widget, a GtkGLArea or a GtkDrawingArea
// that assistive technology sees the content of, and connects its input.
func (s *surface) newArea(gl bool) {
	data := ptr(s.w.id)
	s.area = newSurfaceArea(gl)
	if gl {
		gtkGLAreaSetRequiredVersion(s.area, 3, 3)
		// Only transparent windows show what is behind them. With an alpha
		// channel GTK keeps the area in a texture it blends, rather than in
		// a renderbuffer it copies, which took a small window 50 MB more of
		// the GPU's memory, and an animation half as much CPU again.
		gtkGLAreaSetHasAlpha(s.area, s.w.opts.Transparent)
		connect(s.area, "create-context", cbAreaContext, data)
		connect(s.area, "render", cbSurfaceRender, data)
	}
	s.gl = gl
	gtkWidgetSetCanFocus(s.area, true)
	gtkWidgetAddEvents(s.area, surfaceEvents)
	// The edges of a frameless window resize it, as over a page.
	connect(s.area, "button-press-event", cbButtonPress, data)
	if s.w.undecorated() {
		connect(s.area, "motion-notify-event", cbMotion, data)
	}
	connect(s.area, "draw", cbSurfaceDraw, data)
	connect(s.area, "size-allocate", cbSurfaceSize, data)
	connect(s.area, "realize", cbSurfaceRealize, data)
	connect(s.area, "unrealize", cbSurfaceUnrealize, data)
	connect(s.area, "notify::scale-factor", cbSurfaceScale, data)
	connect(s.area, "button-press-event", cbSurfaceButton, data)
	connect(s.area, "button-release-event", cbSurfaceButton, data)
	connect(s.area, "motion-notify-event", cbSurfaceMotion, data)
	connect(s.area, "leave-notify-event", cbSurfaceLeave, data)
	connect(s.area, "scroll-event", cbSurfaceScroll, data)
	connect(s.area, "key-press-event", cbSurfaceKey, data)
	connect(s.area, "key-release-event", cbSurfaceKey, data)
	connect(s.area, "focus-in-event", cbSurfaceFocusIn, data)
	connect(s.area, "focus-out-event", cbSurfaceFocusOut, data)
}

// contentWidget returns the widget showing the window's content: the web
// view or the surface.
func (w *window) contentWidget() ptr {
	if w.surface != nil {
		return w.surface.area
	}
	return w.web
}

// contentWindow returns the GdkWindow that receives the input of the
// window's content.
func (w *window) contentWindow() ptr {
	if w.surface != nil {
		return w.surface.eventWindow()
	}
	return gtkWidgetGetWindow(w.web)
}

func (s *surface) destroy() {
	gtkIMContextSetClientWindow(s.im, 0)
	gObjectUnref(s.im)
	s.im = 0
	if s.lastKey != 0 {
		gdkEventFree(s.lastKey)
		s.lastKey = 0
	}
}

// popupTrigger returns the last press of a button or, in a surface, of a
// key in the window's content: the event a context menu shows for, whose
// time and serial GTK grabs the pointer with.
func (w *window) popupTrigger() ptr {
	ev := w.press.event
	if s := w.surface; s != nil && s.lastKey != 0 {
		// GdkEventButton and GdkEventKey: time 20.
		if ev == 0 || field[uint32](s.lastKey, 20)-field[uint32](ev, 20) < 1<<31 {
			ev = s.lastKey
		}
	}
	return ev
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
	n := platform.SurfaceNative{Widget: s.area}
	if s.rendering {
		n.GLArea = s.area
	}
	return n
}

// eventWindow returns the GdkWindow that receives the surface's input: the
// drawing area's own, or the input-only window a GtkGLArea, which draws in
// its parent's window, adds when realized.
func (s *surface) eventWindow() ptr {
	win := gtkWidgetGetWindow(s.area)
	if win == 0 || !s.gl {
		return win
	}
	// GList: data 0, next 8.
	for l := gdkWindowPeekChildren(win); l != 0; l = field[ptr](l, 8) {
		child := field[ptr](l, 0)
		var owner ptr
		gdkWindowGetUserData(child, &owner)
		if owner == s.area {
			return child
		}
	}
	return win
}

func (s *surface) Size() (float64, float64, float64) {
	return float64(gtkWidgetGetAllocatedWidth(s.area)), float64(gtkWidgetGetAllocatedHeight(s.area)), float64(max(gtkWidgetGetScaleFactor(s.area), 1))
}

func (s *surface) RequestFrame() {
	if !s.w.closed {
		gtkWidgetQueueDraw(s.area)
	}
}

func (s *surface) PresentPixels(pix []byte, stride, width, height int) {
	if s.cr == 0 {
		if s.rendering {
			// The content draws in memory after all, as when its GL
			// renderer fails: GTK shows only what OpenGL draws.
			s.inMemory = true
			if err := s.present.Present(pix, stride, width, height); err != nil && !s.presentFailed {
				s.presentFailed = true
				log.Printf("mygo: native UI shows nothing: %v", err)
			}
			return
		}
		// Frames are drawn in the draw signal; outside of it, ask for one.
		gtkWidgetQueueDraw(s.area)
		return
	}
	if len(pix) < stride*height || width == 0 || height == 0 {
		return
	}
	const formatARGB32, operatorSource = 0, 1 // premultiplied, native endian: BGRA
	img := cairoImageSurfaceForData(&pix[0], formatARGB32, int32(width), int32(height), int32(stride))
	scale := float64(max(gtkWidgetGetScaleFactor(s.area), 1))
	cairoSurfaceSetDeviceScale(img, scale, scale)
	cairoSetSourceSurface(s.cr, img, 0, 0)
	cairoSetOperator(s.cr, operatorSource)
	cairoPaint(s.cr)
	cairoSurfaceDestroy(img)
}

var cursorNames = map[platform.Cursor]string{
	platform.CursorDefault: "default", platform.CursorPointer: "pointer", platform.CursorText: "text",
	platform.CursorMove: "move", platform.CursorResizeEW: "ew-resize", platform.CursorResizeNS: "ns-resize",
	platform.CursorResizeNWSE: "nwse-resize", platform.CursorResizeNESW: "nesw-resize",
	platform.CursorNotAllowed: "not-allowed", platform.CursorCrosshair: "crosshair",
	platform.CursorGrab: "grab", platform.CursorGrabbing: "grabbing",
	platform.CursorResizeN: "n-resize", platform.CursorResizeE: "e-resize", platform.CursorResizeS: "s-resize",
	platform.CursorResizeW: "w-resize", platform.CursorResizeColumn: "col-resize", platform.CursorResizeRow: "row-resize",
	platform.CursorVerticalText: "vertical-text", platform.CursorCopy: "copy", platform.CursorAlias: "alias",
	platform.CursorContextMenu: "context-menu", platform.CursorNone: "none",
}

func (s *surface) SetCursor(c platform.Cursor) {
	s.cursor = c
	win := s.eventWindow()
	if win == 0 || s.w.cursor.on {
		return // a resize cursor of the edges shows
	}
	cur, ok := surfaceCursors[c]
	if !ok {
		name := cursorNames[c]
		if name == "" {
			name = "default"
		}
		cur = gdkCursorNewFromName(gtkWidgetGetDisplay(s.area), cs(name))
		surfaceCursors[c] = cur
	}
	gdkWindowSetCursor(win, cur)
}

func (s *surface) SetTextInput(t platform.TextInputState) {
	active, caret := t.Active, t.Caret
	s.input = t
	if s.textInput && !active {
		gtkIMContextReset(s.im)
	}
	s.textInput, s.caret = active, caret
	if active {
		r := gdkRectangle{X: int32(caret.X), Y: int32(caret.Y), Width: max(int32(caret.W), 1), Height: int32(caret.H + 0.5)}
		gtkIMContextSetCursorLoc(s.im, &r)
	}
}

func (s *surface) send(ev platform.SurfaceEvent) bool {
	if s.w.closed {
		return false
	}
	return s.w.h.SurfaceEvent(ev)
}

func gdkMods(state uint32) platform.Modifiers {
	var m platform.Modifiers
	if state&1 != 0 {
		m |= platform.ModShift
	}
	if state&(1<<2) != 0 {
		m |= platform.ModCtrl
	}
	if state&(1<<3) != 0 {
		m |= platform.ModAlt
	}
	if state&(1<<26|1<<28) != 0 {
		m |= platform.ModSuper
	}
	return m
}

var gdkKeys = map[uint32]platform.Key{
	0xff0d: platform.KeyEnter, 0xff8d: platform.KeyEnter, 0xff1b: platform.KeyEscape, 0xff08: platform.KeyBackspace,
	0xff09: platform.KeyTab, 0xfe20: platform.KeyTab, 0x20: platform.KeySpace, 0xffff: platform.KeyDelete,
	0xff9f: platform.KeyDelete, 0xff63: platform.KeyInsert, 0xff50: platform.KeyHome, 0xff95: platform.KeyHome,
	0xff57: platform.KeyEnd, 0xff9c: platform.KeyEnd, 0xff55: platform.KeyPageUp, 0xff9a: platform.KeyPageUp,
	0xff56: platform.KeyPageDown, 0xff9b: platform.KeyPageDown, 0xff51: platform.KeyLeft, 0xff96: platform.KeyLeft,
	0xff52: platform.KeyUp, 0xff97: platform.KeyUp, 0xff53: platform.KeyRight, 0xff98: platform.KeyRight,
	0xff54: platform.KeyDown, 0xff99: platform.KeyDown, 0xff67: platform.KeyContextMenu,
}

func keyvalKey(keyval uint32) platform.Key {
	if k, ok := gdkKeys[keyval]; ok {
		return k
	}
	if keyval >= 0xffbe && keyval <= 0xffc9 {
		return platform.KeyF1 + platform.Key(keyval-0xffbe)
	}
	if r := gdkKeyvalToUnicode(keyval); r != 0 {
		return platform.KeyForRune(rune(r))
	}
	return platform.KeyUnknown
}

func (b *Backend) surfaceOf(data ptr) *surface {
	if w := b.window(data); w != nil {
		return w.surface
	}
	return nil
}

func initSurfaceCallbacks() {
	b := func() *Backend { return theBackend }
	cbSurfaceDraw = purego.NewCallback(func(widget, cr, data ptr) bool {
		s := b().surfaceOf(data)
		if s == nil {
			return true
		}
		// A GtkGLArea draws in its render signal, unless it has no context:
		// then, as a drawing area, in this one.
		if s.gl && gtkGLAreaGetError(s.area) == 0 {
			return false
		}
		s.cr = cr
		s.send(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
		s.cr = 0
		return true
	})
	// The GtkGLArea's context: OpenGL 3.3, else OpenGL ES 3.0. One GDK
	// cannot make leaves the area its error, so that it draws with cairo.
	cbAreaContext = purego.NewCallback(func(area, data ptr) ptr {
		ctx, gerr := glContext(gtkWidgetGetWindow(area))
		if gerr != 0 {
			gtkGLAreaSetError(area, gerr)
			gErrorFree(gerr)
		}
		return ctx
	})
	cbSurfaceRender = purego.NewCallback(func(area, context, data ptr) bool {
		s := b().surfaceOf(data)
		if s == nil {
			return false
		}
		s.rendering, s.rendered, s.inMemory = true, true, false
		s.send(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
		s.rendering = false
		return true
	})
	cbSurfaceSize = purego.NewCallback(func(widget, allocation, data ptr) {
		if s := b().surfaceOf(data); s != nil {
			s.send(platform.SurfaceEvent{Kind: platform.SurfaceResize})
		}
	})
	cbSurfaceScale = purego.NewCallback(func(widget, pspec, data ptr) {
		if s := b().surfaceOf(data); s != nil {
			s.send(platform.SurfaceEvent{Kind: platform.SurfaceResize})
			gtkWidgetQueueDraw(s.area)
		}
	})
	// Input methods let go of the GdkWindow before it goes, as GtkEntry
	// has them: fcitx5's disconnects from it.
	cbSurfaceUnrealize = purego.NewCallback(func(widget, data ptr) {
		if s := b().surfaceOf(data); s != nil {
			gtkIMContextSetClientWindow(s.im, 0)
		}
	})
	cbSurfaceRealize = purego.NewCallback(func(widget, data ptr) {
		if s := b().surfaceOf(data); s != nil {
			gtkIMContextSetClientWindow(s.im, s.eventWindow())
			if s.cursor != platform.CursorDefault {
				s.SetCursor(s.cursor) // on the area that took another's place
			}
		}
	})
	// GdkEventButton: type 0, time 20, x 24, y 32, state 48, button 52.
	cbSurfaceButton = purego.NewCallback(func(widget, event, data ptr) bool {
		s := b().surfaceOf(data)
		if s == nil {
			return false
		}
		typ := field[int32](event, 0)
		if typ != 4 && typ != 7 { // GDK_BUTTON_PRESS, GDK_BUTTON_RELEASE: GTK's own double click events add nothing
			return true
		}
		kind := platform.PointerDown
		if typ == 7 {
			kind = platform.PointerUp
		} else if !gtkWidgetHasFocus(s.area) {
			gtkWidgetGrabFocus(s.area)
		}
		button := map[uint32]int{1: 0, 2: 2, 3: 1}[field[uint32](event, 52)]
		s.send(platform.SurfaceEvent{Kind: kind, X: field[float64](event, 24), Y: field[float64](event, 32), Button: button, Mods: gdkMods(field[uint32](event, 48))})
		return true
	})
	// GdkEventMotion: x 24, y 32, state 48.
	cbSurfaceMotion = purego.NewCallback(func(widget, event, data ptr) bool {
		if s := b().surfaceOf(data); s != nil {
			s.send(platform.SurfaceEvent{Kind: platform.PointerMove, X: field[float64](event, 24), Y: field[float64](event, 32), Mods: gdkMods(field[uint32](event, 48))})
		}
		return false
	})
	// GdkEventCrossing: mode 72; only normal crossings leave.
	cbSurfaceLeave = purego.NewCallback(func(widget, event, data ptr) bool {
		if s := b().surfaceOf(data); s != nil && field[int32](event, 72) == 0 {
			s.send(platform.SurfaceEvent{Kind: platform.PointerLeave})
		}
		return false
	})
	// GdkEventScroll: x 24, y 32, state 40, direction 44, delta_x 72, delta_y 80.
	cbSurfaceScroll = purego.NewCallback(func(widget, event, data ptr) bool {
		s := b().surfaceOf(data)
		if s == nil {
			return false
		}
		ev := platform.SurfaceEvent{Kind: platform.PointerScroll, X: field[float64](event, 24), Y: field[float64](event, 32), Mods: gdkMods(field[uint32](event, 40))}
		const step = 50
		switch field[int32](event, 44) {
		case 0:
			ev.DY = -step
		case 1:
			ev.DY = step
		case 2:
			ev.DX = -step
		case 3:
			ev.DX = step
		case 4: // smooth
			dx, dy := field[float64](event, 72), field[float64](event, 80)
			ev.DX, ev.DY = dx*step, dy*step
			ev.Precise = dx != float64(int64(dx)) || dy != float64(int64(dy))
		}
		s.send(ev)
		return true
	})
	// GdkEventKey: type 0, state 24, keyval 28.
	cbSurfaceKey = purego.NewCallback(func(widget, event, data ptr) bool {
		s := b().surfaceOf(data)
		if s == nil {
			return false
		}
		kind := platform.KeyPressed
		if field[int32](event, 0) == 9 { // GDK_KEY_RELEASE
			kind = platform.KeyReleased
		} else {
			if s.lastKey != 0 {
				gdkEventFree(s.lastKey)
			}
			s.lastKey = gdkEventCopy(event)
		}
		if s.textInput && gtkIMContextFilterKeypress(s.im, event) {
			return true
		}
		k := keyvalKey(field[uint32](event, 28))
		if k == platform.KeyUnknown {
			return false
		}
		s.send(platform.SurfaceEvent{Kind: kind, Key: k, Mods: gdkMods(field[uint32](event, 24))})
		return true
	})
	cbSurfaceFocusIn = purego.NewCallback(func(widget, event, data ptr) bool {
		if s := b().surfaceOf(data); s != nil {
			gtkIMContextFocusIn(s.im)
			s.send(platform.SurfaceEvent{Kind: platform.SurfaceFocus})
		}
		return false
	})
	cbSurfaceFocusOut = purego.NewCallback(func(widget, event, data ptr) bool {
		if s := b().surfaceOf(data); s != nil {
			gtkIMContextFocusOut(s.im)
			s.send(platform.SurfaceEvent{Kind: platform.SurfaceBlur})
		}
		return false
	})
	cbIMCommit = purego.NewCallback(func(im, str, data ptr) {
		if s := b().surfaceOf(data); s != nil {
			if text := goStr(str); text != "" {
				s.send(platform.SurfaceEvent{Kind: platform.TextInput, Text: text})
			}
		}
	})
	cbIMPreedit = purego.NewCallback(func(im, data ptr) {
		s := b().surfaceOf(data)
		if s == nil {
			return
		}
		var str ptr
		var cursor int32
		gtkIMContextGetPreedit(s.im, &str, nil, &cursor)
		text := takeStr(str)
		s.send(platform.SurfaceEvent{Kind: platform.TextComposition, Text: text, Caret: min(int(cursor), utf8.RuneCountInString(text))})
	})
	cbIMPreeditEnd = purego.NewCallback(func(im, data ptr) {
		if s := b().surfaceOf(data); s != nil {
			s.send(platform.SurfaceEvent{Kind: platform.TextComposition})
		}
	})
}
