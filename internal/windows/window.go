//go:build windows && (amd64 || arm64)

package windows

import (
	"errors"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

var errDestroyed = errors.New("mygo: window has been destroyed")

type window struct {
	b      *Backend
	h      platform.WindowHandler
	opts   *platform.WindowOptions
	hwnd   uintptr
	parent *window

	// The WebView2 controller is created asynchronously; calls that need
	// it wait in pending until it exists.
	controller uintptr // ICoreWebView2Controller
	webview    uintptr // ICoreWebView2
	settings   uintptr // ICoreWebView2Settings
	ready      bool
	pending    []func()
	closed     bool
	destroying bool

	minW, minH, maxW, maxH int // DIPs
	movable, closable      bool
	frameless              bool
	fullScreen             bool
	saved                  struct {
		style, exStyle uintptr
		placement      windowPlacement
	}
	state       int // last sizeRestored, sizeMinimized or sizeMaximized
	opacity     float64
	bgBrush     uintptr
	bg          *platform.Color
	vibrancy    string
	shadow      bool
	ignoreMouse bool

	menu    *platform.Menu // menu bar
	hmenu   uintptr
	owner   int
	ownMenu bool
	accels  map[accelKey]uint16

	programmatic bool
	loading      bool
	zoom         float64
	devTools     bool
	htmlFor      map[string]string // LoadHTML documents by the URL they load at
	calls        map[int]func(string, error)
	nextCall     int
}

func (b *Backend) NewWindow(o *platform.WindowOptions, h platform.WindowHandler) (platform.Window, error) {
	w := &window{
		b: b, h: h, opts: o,
		movable: o.Movable, closable: o.Closable, frameless: o.Frameless,
		opacity: 1, zoom: 1, shadow: o.HasShadow,
		minW: o.MinSize.Width, minH: o.MinSize.Height, maxW: o.MaxSize.Width, maxH: o.MaxSize.Height,
		htmlFor: map[string]string{}, calls: map[int]func(string, error){},
	}
	var owner uintptr
	if p, ok := o.Parent.(*window); ok && p != nil && !p.closed {
		w.parent, owner = p, p.hwnd
	}
	style, ex := w.styles()
	w.hwnd = createWindow(ex, windowClass, o.Title, style, 0, 0, 0, 0, owner)
	if w.hwnd == 0 {
		return nil, errors.New("mygo: cannot create a window")
	}
	b.windows[w.hwnd] = w
	w.placeInitially()

	if !o.Closable {
		w.SetClosable(false)
	}
	if o.AlwaysOnTop {
		w.SetAlwaysOnTop(true)
	}
	if o.Opacity > 0 && o.Opacity < 1 {
		w.SetOpacity(o.Opacity)
	}
	if o.BackgroundColor != nil {
		w.SetBackgroundColor(*o.BackgroundColor)
	}
	if w.frameless && w.shadow {
		w.SetHasShadow(true)
	}
	b.applyWindowTheme(w)
	if o.Vibrancy != "" {
		w.SetVibrancy(o.Vibrancy)
	}
	if b.icon != 0 {
		w.setIcon(b.icon)
	}
	if b.appMenu != nil {
		w.installMenu(b.appMenu)
	}
	if o.Modal && w.parent != nil {
		procEnableWindow.Call(w.parent.hwnd, 0)
	}
	if o.FullScreen {
		w.SetFullScreen(true)
	}
	b.whenEnvironment(w.createWebView)
	return w, nil
}

func (w *window) styles() (style, ex uint32) {
	o := w.opts
	style = wsOverlappedWindow | wsClipChildren
	if !o.Resizable {
		style &^= wsThickFrame | wsMaximizeBox
	}
	if !o.Minimizable {
		style &^= wsMinimizeBox
	}
	if !o.Maximizable {
		style &^= wsMaximizeBox
	}
	if o.SkipTaskbar {
		ex |= wsExToolWindow
	}
	if !o.Focusable {
		ex |= wsExNoActivate
	}
	return style, ex
}

// placeInitially sizes and positions a new window, in the DPI of the
// monitor it appears on.
func (w *window) placeInitially() {
	o := w.opts
	var mon uintptr
	if o.Center {
		mon, _, _ = procMonitorFromPoint.Call(0, monitorDefaultToPrimary)
	} else {
		mon = monitorAt(o.X, o.Y)
	}
	dpi := monitorDPI(mon)
	width, height := toPx(o.Width, dpi), toPx(o.Height, dpi)
	if o.UseContentSize && !w.frameless {
		width, height = w.outerSize(width, height, dpi)
	}
	x, y := toPx(o.X, dpi), toPx(o.Y, dpi)
	if o.Center {
		work := monitorWorkArea(mon)
		x = work.Left + (work.Right-work.Left-width)/2
		y = work.Top + (work.Bottom-work.Top-height)/2
	}
	procSetWindowPos.Call(w.hwnd, 0, uintptr(x), uintptr(y), uintptr(width), uintptr(height), swpNoZOrder|swpNoActivate)
}

// outerSize returns the window size for a content size, in pixels.
func (w *window) outerSize(width, height int32, dpi int) (int32, int32) {
	r := rect{0, 0, width, height}
	menu := uintptr(0)
	if w.hmenu != 0 || (w.hmenu == 0 && w.b.appMenu != nil) {
		menu = 1
	}
	style, ex := windowLong(w.hwnd, gwlStyle), windowLong(w.hwnd, gwlExStyle)
	if has(procAdjustWindowRectExForDpi) {
		procAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(&r)), style, menu, ex, uintptr(dpi))
	}
	return r.Right - r.Left, r.Bottom - r.Top
}

func monitorAt(x, y int) uintptr {
	pt := uintptr(uint32(int32(x))) | uintptr(uint32(int32(y)))<<32
	m, _, _ := procMonitorFromPoint.Call(pt, monitorDefaultToNearest)
	return m
}

func monitorDPI(mon uintptr) int {
	if mon != 0 && has(procGetDpiForMonitor) {
		var dx, dy uint32
		if r, _, _ := procGetDpiForMonitor.Call(mon, 0, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); r == 0 && dx != 0 {
			return int(dx)
		}
	}
	return dpiOf(0)
}

func monitorInfo(mon uintptr) monitorInfoEx {
	var mi monitorInfoEx
	mi.Size = uint32(unsafe.Sizeof(mi))
	procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	return mi
}

func monitorWorkArea(mon uintptr) rect { return monitorInfo(mon).Work }

func (w *window) monitor() uintptr {
	m, _, _ := procMonitorFromWindow.Call(w.hwnd, monitorDefaultToNearest)
	return m
}

// message handles a window message; ok false lets DefWindowProc run.
func (w *window) message(m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case wmClose:
		if w.h.ShouldClose() {
			w.destroy()
		}
		return 0, true
	case wmDestroy:
		w.cleanup()
		w.h.Closed()
		return 0, true
	case wmSize:
		w.resizeWebView()
		switch wp {
		case sizeMinimized:
			if w.state != sizeMinimized {
				w.state = sizeMinimized
				w.h.Minimized()
			}
		case sizeMaximized:
			if w.state != sizeMaximized {
				if w.state == sizeMinimized {
					w.h.Restored()
				}
				w.state = sizeMaximized
				w.h.Maximized()
			}
		case sizeRestored:
			switch w.state {
			case sizeMinimized:
				w.h.Restored()
			case sizeMaximized:
				w.h.Unmaximized()
			}
			w.state = sizeRestored
		}
		w.h.Resized()
		return 0, true
	case wmMove:
		if w.controller != 0 {
			comCall(w.controller, ctlNotifyParentWindowPositionChanged)
		}
		w.h.Moved()
		return 0, true
	case wmActivate:
		if loword(wp) == waInactive {
			w.h.Blurred()
		} else {
			w.h.Focused()
		}
		return 0, false
	case wmSetFocus:
		w.focusWebView()
		return 0, true
	case wmGetMinMaxInfo:
		info := (*minMaxInfo)(native(lp))
		dpi := dpiOf(w.hwnd)
		if w.minW > 0 || w.minH > 0 {
			info.MinTrackSize = point{toPx(w.minW, dpi), toPx(w.minH, dpi)}
		}
		if w.maxW > 0 {
			info.MaxTrackSize.X = toPx(w.maxW, dpi)
		}
		if w.maxH > 0 {
			info.MaxTrackSize.Y = toPx(w.maxH, dpi)
		}
		return 0, true
	case wmDpiChanged:
		r := (*rect)(native(lp))
		procSetWindowPos.Call(w.hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpNoZOrder|swpNoActivate)
		return 0, true
	case wmNCCalcSize:
		if w.frameless && wp != 0 && !w.fullScreen {
			return w.frameCalcSize(wp, lp), true
		}
	case wmEraseBkgnd:
		if w.bgBrush != 0 && w.vibrancy == "" {
			var r rect
			procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
			procFillRect.Call(wp, uintptr(unsafe.Pointer(&r)), w.bgBrush)
		}
		return 1, true
	case wmWindowPosChanging:
		if !w.movable {
			pos := (*windowPos)(native(lp))
			pos.Flags |= swpNoMove
		}
		return 0, false
	case wmCommand:
		if hiword(wp) == 0 && lp == 0 { // a menu item
			w.b.menuCommand(loword(wp), w)
			return 0, true
		}
	}
	return 0, false
}

// frameCalcSize removes the title bar of frameless windows. Resizable ones
// keep their left, right and bottom borders, which Windows 10 and later
// draw invisible outside the window, so they still resize. Maximized
// windows would overflow the screen by their borders: fit the work area.
func (w *window) frameCalcSize(wp, lp uintptr) uintptr {
	params := (*ncCalcSizeParams)(native(lp))
	if w.IsMaximized() {
		params.Rgrc[0] = monitorWorkArea(w.monitor())
		return 0
	}
	if windowLong(w.hwnd, gwlStyle)&wsThickFrame != 0 {
		top := params.Rgrc[0].Top
		procDefWindowProcW.Call(w.hwnd, wmNCCalcSize, wp, lp)
		params.Rgrc[0].Top = top
	}
	return 0
}

func (w *window) destroy() {
	if w.closed || w.destroying {
		return
	}
	w.destroying = true
	// Re-enable the owner first, or Windows activates another app.
	if w.opts.Modal && w.parent != nil && !w.parent.closed {
		procEnableWindow.Call(w.parent.hwnd, 1)
	}
	procDestroyWindow.Call(w.hwnd)
}

func (w *window) cleanup() {
	if w.closed {
		return
	}
	w.closed = true
	if w.opts.Modal && w.parent != nil && !w.parent.closed {
		procEnableWindow.Call(w.parent.hwnd, 1)
	}
	for id, cb := range w.calls {
		delete(w.calls, id)
		cb("", errDestroyed)
	}
	w.pending = nil
	if w.controller != 0 {
		comCall(w.controller, ctlClose)
		release(w.settings)
		release(w.webview)
		release(w.controller)
		w.controller, w.webview, w.settings = 0, 0, 0
	}
	w.b.menus.drop(w.owner)
	if w.bgBrush != 0 {
		procDeleteObject.Call(w.bgBrush)
		w.bgBrush = 0
	}
	delete(w.b.windows, w.hwnd)
}

func (w *window) Handle() uintptr        { return w.hwnd }
func (w *window) WebViewHandle() uintptr { return w.webview }

func (w *window) SetTitle(title string) {
	procSetWindowTextW.Call(w.hwnd, uintptr(unsafe.Pointer(u16(title))))
}

func (w *window) Title() string {
	n, _, _ := procGetWindowTextLengthW.Call(w.hwnd)
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(w.hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return string(utf16Decode(buf))
}

func utf16Decode(b []uint16) []rune {
	for i, c := range b {
		if c == 0 {
			b = b[:i]
			break
		}
	}
	out := make([]rune, 0, len(b))
	for i := 0; i < len(b); i++ {
		c := rune(b[i])
		if c >= 0xD800 && c < 0xDC00 && i+1 < len(b) {
			c = (c-0xD800)<<10 + (rune(b[i+1]) - 0xDC00) + 0x10000
			i++
		}
		out = append(out, c)
	}
	return out
}

func (w *window) rectToDIP(r rect) platform.Rect {
	dpi := dpiOf(w.hwnd)
	return platform.Rect{X: toDIP(r.Left, dpi), Y: toDIP(r.Top, dpi), Width: toDIP(r.Right-r.Left, dpi), Height: toDIP(r.Bottom-r.Top, dpi)}
}

func (w *window) SetBounds(r platform.Rect) {
	dpi := dpiOf(w.hwnd)
	procSetWindowPos.Call(w.hwnd, 0, uintptr(toPx(r.X, dpi)), uintptr(toPx(r.Y, dpi)), uintptr(toPx(r.Width, dpi)), uintptr(toPx(r.Height, dpi)), swpNoZOrder|swpNoActivate)
}

func (w *window) Bounds() platform.Rect {
	var r rect
	procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	return w.rectToDIP(r)
}

func (w *window) SetContentBounds(r platform.Rect) {
	dpi := dpiOf(w.hwnd)
	outer := rect{toPx(r.X, dpi), toPx(r.Y, dpi), toPx(r.X+r.Width, dpi), toPx(r.Y+r.Height, dpi)}
	if !w.frameless && has(procAdjustWindowRectExForDpi) {
		menu := uintptr(0)
		if w.hmenu != 0 {
			menu = 1
		}
		procAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(&outer)), windowLong(w.hwnd, gwlStyle), menu, windowLong(w.hwnd, gwlExStyle), uintptr(dpi))
	}
	procSetWindowPos.Call(w.hwnd, 0, uintptr(outer.Left), uintptr(outer.Top), uintptr(outer.Right-outer.Left), uintptr(outer.Bottom-outer.Top), swpNoZOrder|swpNoActivate)
}

func (w *window) ContentBounds() platform.Rect {
	var r rect
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	var origin point
	procClientToScreen.Call(w.hwnd, uintptr(unsafe.Pointer(&origin)))
	return w.rectToDIP(rect{origin.X, origin.Y, origin.X + r.Right, origin.Y + r.Bottom})
}

func (w *window) SetMinimumSize(s platform.Size) { w.minW, w.minH = s.Width, s.Height }
func (w *window) SetMaximumSize(s platform.Size) { w.maxW, w.maxH = s.Width, s.Height }

func (w *window) setStyle(bits uint32, on bool) {
	style := windowLong(w.hwnd, gwlStyle)
	if on {
		style |= uintptr(bits)
	} else {
		style &^= uintptr(bits)
	}
	setWindowLong(w.hwnd, gwlStyle, style)
	procSetWindowPos.Call(w.hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
}

func (w *window) setExStyle(bits uint32, on bool) {
	ex := windowLong(w.hwnd, gwlExStyle)
	if on {
		ex |= uintptr(bits)
	} else {
		ex &^= uintptr(bits)
	}
	setWindowLong(w.hwnd, gwlExStyle, ex)
}

func (w *window) hasStyle(bits uint32) bool { return windowLong(w.hwnd, gwlStyle)&uintptr(bits) != 0 }

func (w *window) SetResizable(v bool) {
	bits := uint32(wsThickFrame)
	if w.opts.Maximizable {
		bits |= wsMaximizeBox
	}
	w.setStyle(bits, v)
}

func (w *window) IsResizable() bool     { return w.hasStyle(wsThickFrame) }
func (w *window) SetMovable(v bool)     { w.movable = v }
func (w *window) IsMovable() bool       { return w.movable }
func (w *window) SetMinimizable(v bool) { w.setStyle(wsMinimizeBox, v) }
func (w *window) IsMinimizable() bool   { return w.hasStyle(wsMinimizeBox) }
func (w *window) SetMaximizable(v bool) { w.setStyle(wsMaximizeBox, v) }
func (w *window) IsMaximizable() bool   { return w.hasStyle(wsMaximizeBox) }
func (w *window) IsClosable() bool      { return w.closable }

func (w *window) SetClosable(v bool) {
	w.closable = v
	menu, _, _ := procGetSystemMenu.Call(w.hwnd, 0)
	flags := uintptr(mfByCommand)
	if !v {
		flags |= mfGrayed
	}
	procEnableMenuItem.Call(menu, scClose, flags)
}

func (w *window) SetAlwaysOnTop(v bool) {
	after := hwndNoTopmost
	if v {
		after = hwndTopmost
	}
	procSetWindowPos.Call(w.hwnd, after, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
}

func (w *window) IsAlwaysOnTop() bool { return windowLong(w.hwnd, gwlExStyle)&wsExTopmost != 0 }

func (w *window) Show() {
	procShowWindow.Call(w.hwnd, swShow)
	procSetForegroundWindow.Call(w.hwnd)
}

func (w *window) ShowInactive() { procShowWindow.Call(w.hwnd, swShowNoActivate) }
func (w *window) Hide()         { procShowWindow.Call(w.hwnd, swHide) }

func (w *window) IsVisible() bool {
	r, _, _ := procIsWindowVisible.Call(w.hwnd)
	return r != 0
}

func (w *window) Focus() {
	if w.IsMinimized() {
		procShowWindow.Call(w.hwnd, swRestore)
	}
	procSetForegroundWindow.Call(w.hwnd)
	w.focusWebView()
}

func (w *window) Blur() {
	const gwHwndNext = 2
	next, _, _ := procGetNextWindow.Call(w.hwnd, gwHwndNext)
	if next != 0 {
		procSetForegroundWindow.Call(next)
	}
}

func (w *window) IsFocused() bool {
	fg, _, _ := procGetForegroundWindow.Call()
	return fg == w.hwnd
}

func (w *window) Minimize() { procShowWindow.Call(w.hwnd, swMinimize) }

func (w *window) IsMinimized() bool {
	r, _, _ := procIsIconic.Call(w.hwnd)
	return r != 0
}

func (w *window) Maximize() { procShowWindow.Call(w.hwnd, swShowMaximized) }

func (w *window) Unmaximize() {
	if w.IsMaximized() {
		procShowWindow.Call(w.hwnd, swRestore)
	}
}

func (w *window) IsMaximized() bool {
	r, _, _ := procIsZoomed.Call(w.hwnd)
	return r != 0
}

func (w *window) Restore() { procShowWindow.Call(w.hwnd, swRestore) }

func (w *window) SetFullScreen(v bool) {
	if v == w.fullScreen {
		return
	}
	if v {
		w.saved.style = windowLong(w.hwnd, gwlStyle)
		w.saved.exStyle = windowLong(w.hwnd, gwlExStyle)
		w.saved.placement.Length = uint32(unsafe.Sizeof(w.saved.placement))
		procGetWindowPlacement.Call(w.hwnd, uintptr(unsafe.Pointer(&w.saved.placement)))
		w.fullScreen = true
		style := w.saved.style &^ (wsCaption | wsThickFrame)
		setWindowLong(w.hwnd, gwlStyle, style)
		m := monitorInfo(w.monitor()).Monitor
		procSetWindowPos.Call(w.hwnd, 0, uintptr(m.Left), uintptr(m.Top), uintptr(m.Right-m.Left), uintptr(m.Bottom-m.Top), swpNoZOrder|swpFrameChanged)
		w.h.EnteredFullScreen()
		return
	}
	w.fullScreen = false
	setWindowLong(w.hwnd, gwlStyle, w.saved.style)
	setWindowLong(w.hwnd, gwlExStyle, w.saved.exStyle)
	procSetWindowPlacement.Call(w.hwnd, uintptr(unsafe.Pointer(&w.saved.placement)))
	procSetWindowPos.Call(w.hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpFrameChanged)
	w.h.LeftFullScreen()
}

func (w *window) IsFullScreen() bool { return w.fullScreen }

func (w *window) Center() {
	var r rect
	procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	work := monitorWorkArea(w.monitor())
	width, height := r.Right-r.Left, r.Bottom-r.Top
	x := work.Left + (work.Right-work.Left-width)/2
	y := work.Top + (work.Bottom-work.Top-height)/2
	procSetWindowPos.Call(w.hwnd, 0, uintptr(x), uintptr(y), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
}

func (w *window) SetBackgroundColor(c platform.Color) {
	w.bg = &c
	if w.bgBrush != 0 {
		procDeleteObject.Call(w.bgBrush)
	}
	w.bgBrush, _, _ = procCreateSolidBrush.Call(uintptr(c.R) | uintptr(c.G)<<8 | uintptr(c.B)<<16)
	w.withWebView(w.applyWebViewBackground)
	procInvalidateRect(w.hwnd)
}

// applyWebViewBackground paints the webview before its page does:
// transparent with a material behind it, else the background color.
// WebView2 only supports fully opaque or fully transparent backgrounds.
func (w *window) applyWebViewBackground() {
	ctl2 := queryInterface(w.controller, &iidICoreWebView2Controller2)
	if ctl2 == 0 {
		return
	}
	defer release(ctl2)
	var c uint32 = 0xFFFFFFFF // opaque white: A, R, G, B bytes
	switch {
	case w.vibrancy != "" || w.opts.Transparent:
		c = 0
	case w.bg != nil && w.bg.A == 0:
		c = 0
	case w.bg != nil:
		c = 0xFF | uint32(w.bg.R)<<8 | uint32(w.bg.G)<<16 | uint32(w.bg.B)<<24
	}
	comCall(ctl2, ctl2PutDefaultBackgroundColor, uintptr(c))
}

func (w *window) SetOpacity(v float64) {
	w.opacity = v
	w.updateLayered()
}

func (w *window) Opacity() float64 { return w.opacity }

// updateLayered makes the window layered while it is translucent or lets
// the mouse through.
func (w *window) updateLayered() {
	layered := w.opacity < 1 || w.ignoreMouse
	w.setExStyle(wsExLayered, layered)
	if layered {
		procSetLayeredWindowAttributes.Call(w.hwnd, 0, uintptr(byte(w.opacity*255+0.5)), lwaAlpha)
	}
}

func (w *window) SetHasShadow(v bool) {
	w.shadow = v
	if !w.frameless || w.vibrancy != "" {
		return
	}
	m := margins{}
	if v {
		m.Top = 1 // a frame the size of a pixel brings the shadow back
	}
	procDwmExtendFrameIntoClientArea.Call(w.hwnd, uintptr(unsafe.Pointer(&m)))
}

func (w *window) HasShadow() bool { return w.shadow }

func (w *window) SetIgnoreMouseEvents(v bool) {
	w.ignoreMouse = v
	w.setExStyle(wsExTransparent, v)
	w.updateLayered()
}

func (w *window) SetContentProtection(v bool) {
	if !has(procSetWindowDisplayAffinity) {
		return
	}
	affinity := uintptr(wdaNone)
	if v {
		affinity = wdaExcludeFromCapture
	}
	if r, _, _ := procSetWindowDisplayAffinity.Call(w.hwnd, affinity); r == 0 && v {
		procSetWindowDisplayAffinity.Call(w.hwnd, 1) // WDA_MONITOR before Windows 10 2004
	}
}

// Windows 11 system backdrops (DWM_SYSTEMBACKDROP_TYPE).
const (
	backdropNone    = 1
	backdropMica    = 2
	backdropAcrylic = 3
	backdropMicaAlt = 4
)

func backdropFor(material string) int32 {
	switch material {
	case "":
		return backdropNone
	case "mica":
		return backdropMica
	case "tabbed":
		return backdropMicaAlt
	case "acrylic", "menu", "popover", "hud", "sheet", "tooltip", "selection", "fullscreen-ui":
		return backdropAcrylic
	}
	return backdropMica
}

func (w *window) SetVibrancy(material string) {
	w.vibrancy = material
	backdrop := backdropFor(material)
	m := margins{}
	if material != "" {
		// The material shows where the frame extends, behind the page.
		m = margins{-1, -1, -1, -1}
	}
	procDwmExtendFrameIntoClientArea.Call(w.hwnd, uintptr(unsafe.Pointer(&m)))
	procDwmSetWindowAttribute.Call(w.hwnd, dwmwaSystemBackdropType, uintptr(unsafe.Pointer(&backdrop)), 4)
	if material == "" && w.frameless && w.shadow {
		w.SetHasShadow(true)
	}
	w.withWebView(w.applyWebViewBackground)
	procInvalidateRect(w.hwnd)
}

func (w *window) SetMenu(m *platform.Menu) {
	w.ownMenu = m != nil
	if m == nil && w.b.appMenu != nil {
		m = w.b.appMenu
	}
	w.installMenu(m)
}

func (w *window) StartDrag() {
	procReleaseCapture.Call()
	procSendMessageW.Call(w.hwnd, wmNCLButtonDown, htCaption, 0)
}

func (w *window) TitleBarDoubleClicked() {
	if w.IsMaximized() {
		w.Unmaximize()
	} else if w.hasStyle(wsMaximizeBox) {
		w.Maximize()
	}
}

func (w *window) Close() {
	if !w.closed {
		w.destroy()
	}
}

func (w *window) setIcon(icon uintptr) {
	const iconSmall, iconBig = 0, 1
	procSendMessageW.Call(w.hwnd, wmSetIcon, iconSmall, icon)
	procSendMessageW.Call(w.hwnd, wmSetIcon, iconBig, icon)
}

func procInvalidateRect(hwnd uintptr) { procInvalidateRectW.Call(hwnd, 0, 1) }

func boolArg(v bool) uintptr {
	if v {
		return 1
	}
	return 0
}
