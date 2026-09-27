package mygo

import (
	"context"
	"crypto/rand"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/internal/bridge"
	"github.com/egoist/mygo/internal/platform"
)

// TitleBarStyle selects how the title bar of a framed window looks (macOS).
type TitleBarStyle string

// Title bar styles.
const (
	TitleBarDefault TitleBarStyle = "default"
	// TitleBarHidden extends the page under a transparent title bar while
	// keeping the window controls.
	TitleBarHidden TitleBarStyle = "hidden"
	// TitleBarHiddenInset is TitleBarHidden with the window controls inset
	// further from the edges.
	TitleBarHiddenInset TitleBarStyle = "hiddenInset"
)

// DevTools selects whether the web inspector is available.
type DevTools int

// DevTools modes.
const (
	// DevToolsAuto enables the inspector unless the app was built for
	// production with `mygo build`.
	DevToolsAuto DevTools = iota
	DevToolsEnabled
	DevToolsDisabled
)

// Point is a position in screen coordinates.
type Point struct{ X, Y int }

// Size is a width and height in DIPs.
type Size struct{ Width, Height int }

// Rectangle is a position and size in screen coordinates (DIPs, origin at
// the top-left corner of the primary display).
type Rectangle struct{ X, Y, Width, Height int }

// WindowOptions configures NewWindow. The zero value is a visible,
// resizable 800x600 window centered on screen.
type WindowOptions struct {
	// Title of the window. Defaults to the application name. The title
	// follows the page's <title> unless an OnPageTitleUpdated listener
	// prevents it.
	Title string
	// URL is loaded right after the window is created. "/" and other URLs
	// without a scheme are pages of the app's frontend (see LoadURL).
	URL string

	// Width and Height of the window in DIPs (default 800x600).
	Width, Height int
	// UseContentSize makes Width and Height describe the page area instead
	// of the whole window.
	UseContentSize bool
	// X and Y of the top-left corner. The window is centered when both are
	// zero.
	X, Y                int
	MinWidth, MinHeight int
	MaxWidth, MaxHeight int

	// Hidden creates the window without showing it. Call Show, typically
	// from OnReadyToShow, to avoid a blank window while the page loads.
	Hidden bool
	// Frameless removes the title bar and window chrome. Mark draggable
	// areas with the CSS `--app-region: drag`.
	Frameless bool
	// TitleBarStyle hides the title bar but keeps the window controls
	// (macOS).
	TitleBarStyle TitleBarStyle
	// TrafficLightPosition moves the window controls of a window with a
	// hidden title bar (macOS).
	TrafficLightPosition *Point

	DisableResize     bool
	DisableMove       bool
	DisableMinimize   bool
	DisableMaximize   bool
	DisableClose      bool
	DisableFullScreen bool
	DisableShadow     bool
	AlwaysOnTop       bool
	FullScreen        bool
	// Maximized creates the window maximized.
	Maximized bool
	// SkipTaskbar hides the window from the taskbar (Linux, Windows).
	SkipTaskbar bool
	// Transparent makes the window background transparent, so a page with a
	// transparent background shows the desktop through.
	Transparent bool
	// BackgroundColor fills the window until the page paints, which avoids
	// a flash of another color while it loads. CSS syntax: "#1e1e1e",
	// "#rgba", "rgb(30 30 30)", or "light-dark(#f5f5f7, #1e1e1e)" for a
	// page that follows the light or dark appearance.
	BackgroundColor string
	// Vibrancy puts a translucent, blurred material behind a transparent
	// page (macOS and Windows 11), e.g. VibrancySidebar.
	Vibrancy Vibrancy
	// Opacity of the window between 0 and 1 (default 1).
	Opacity float64
	// Parent makes this window a child window that stays on top of it.
	Parent *Window
	// Modal makes a child window modal to its Parent.
	Modal bool

	// PreloadScript is JavaScript injected into every page before the
	// page's own scripts, after window.mygo is available.
	PreloadScript string
	// TrustedOrigins lists extra origins, such as "https://example.com",
	// whose pages may call bound Go methods. By default only the app's own
	// content can: custom schemes registered with Protocol, file: and
	// about: pages, and loopback dev servers during development. "*"
	// trusts every origin.
	TrustedOrigins []string
	// DevTools controls the web inspector.
	DevTools DevTools
	// ZoomFactor of the page (default 1).
	ZoomFactor float64
	// UserAgent overrides the user agent string.
	UserAgent string
	// StateKey remembers the window's position, size, and maximized and
	// full screen state under this key, in window-state.json in
	// PathUserData. A window created with the same key, for example at the
	// next launch, gets them back as long as it would show on a connected
	// display: they override X, Y, Width, Height, UseContentSize,
	// Maximized and FullScreen.
	StateKey string
}

// Window is a native window hosting a web page. Create windows with
// NewWindow once the application is ready.
type Window struct {
	id     int
	parent *Window
	// secret starts every message of the bridge in this window's pages.
	// Only the bridge knows it, so messages posted from elsewhere (an
	// iframe of another origin) are ignored.
	secret string

	// native is only accessed on the main thread; nil once destroyed.
	native    platform.Window
	destroyed atomic.Bool
	shown     bool
	readyShow bool
	// background is the BackgroundColor, if any. Main thread only.
	background *background
	menu       *Menu
	// stateKey is WindowOptions.StateKey; stateTimer captures the state
	// once it settled. Main thread only.
	stateKey   string
	stateTimer *time.Timer

	// trusted reports whether the current page may call bound methods.
	// Main thread only.
	trusted        bool
	trustedOrigins []string

	mu                sync.Mutex
	openHandler       func(WindowOpenRequest) *WindowOptions
	permissionHandler func(PermissionRequest) bool
	pageCtx           context.Context
	pageCancel        context.CancelFunc

	outMu    sync.Mutex
	outbox   [][]byte
	flushing bool
	// held keeps events until the page's DOM is ready, so events sent right
	// after creating a window or during a navigation are not lost.
	held     [][]byte
	domReady bool

	onClose            listeners[func(*CloseEvent)]
	onClosed           listeners[func()]
	onFocus            listeners[func()]
	onBlur             listeners[func()]
	onShow             listeners[func()]
	onHide             listeners[func()]
	onReadyToShow      listeners[func()]
	onResize           listeners[func()]
	onFileDrop         listeners[func(*FileDropEvent)]
	onWillDownload     listeners[func(*DownloadEvent)]
	onDownloadDone     listeners[func(*Download)]
	onMove             listeners[func()]
	onMaximize         listeners[func()]
	onUnmaximize       listeners[func()]
	onMinimize         listeners[func()]
	onRestore          listeners[func()]
	onEnterFullScreen  listeners[func()]
	onLeaveFullScreen  listeners[func()]
	onPageTitleUpdated listeners[func(*TitleEvent)]
	onWillNavigate     listeners[func(*NavigateEvent)]
	onDidNavigate      listeners[func(string)]
	onDOMReady         listeners[func()]
	onDidFinishLoad    listeners[func()]
	onDidFailLoad      listeners[func(*LoadError)]
	onRenderGone       listeners[func(string)]
}

// LoadError describes a failed page load.
type LoadError struct {
	URL         string
	Code        int
	Description string
}

func (e *LoadError) Error() string {
	return fmt.Sprintf("mygo: failed to load %s: %s (%d)", e.URL, e.Description, e.Code)
}

// WindowOpenRequest describes a window.open() call or a click on a
// target="_blank" link.
type WindowOpenRequest struct {
	URL       string
	FrameName string
	// Width and Height requested through window.open() features; 0 when
	// not specified.
	Width, Height int
}

var windows struct {
	sync.Mutex
	nextID  int
	list    []*Window
	focused *Window
}

// NewWindow creates a window. It must be called after the application is
// ready (see Application.WhenReady); when called from another goroutine it
// waits for that.
func NewWindow(opts WindowOptions) *Window {
	// Validate in the caller's goroutine so mistakes point at the call.
	bg, err := backgroundOption(opts.BackgroundColor)
	if err != nil {
		panic(err)
	}
	if !isMainThread() {
		App.waitReady()
	} else if !App.IsReady() {
		panic("mygo: NewWindow called before the application is ready; create windows in App.WhenReady")
	}
	var w *Window
	onMain(func() { w = newWindow(opts, bg, 0) })
	if w == nil {
		// The event loop has already stopped.
		w = &Window{}
		w.destroyed.Store(true)
	}
	return w
}

func backgroundOption(s string) (*background, error) {
	if s == "" {
		return nil, nil
	}
	bg, err := parseBackground(s)
	if err != nil {
		return nil, err
	}
	return &bg, nil
}

// backgroundColor returns the window's background for the current
// appearance, or nil. Main thread only.
func (w *Window) backgroundColor() *platform.Color {
	if w.background == nil {
		return nil
	}
	c := w.background.light
	if w.background.dark != c && backend().Theme().IsDark() {
		c = w.background.dark
	}
	return &c
}

// updateBackgrounds gives windows with a light and a dark background the
// one of the current appearance. Main thread only.
func updateBackgrounds() {
	for _, w := range Windows() {
		if bg := w.background; w.native != nil && bg != nil && bg.light != bg.dark {
			w.native.SetBackgroundColor(*w.backgroundColor())
		}
	}
}

func newWindow(opts WindowOptions, bg *background, native uintptr) *Window {
	windows.Lock()
	windows.nextID++
	id := windows.nextID
	windows.Unlock()

	w := &Window{id: id, parent: opts.Parent, trustedOrigins: opts.TrustedOrigins, secret: rand.Text(), stateKey: opts.StateKey, background: bg}
	w.resetPage()
	popts := w.platformOptions(&opts)
	popts.BackgroundColor = w.backgroundColor()
	if opts.Transparent {
		// Backends keep transparent windows clear; so do theme changes.
		w.background = nil
	}
	popts.Native = native
	if w.stateKey != "" {
		restoreWindowState(w.stateKey, popts)
	}
	nw, err := backend().NewWindow(popts, &windowHandler{w})
	if err != nil {
		panic(fmt.Sprintf("mygo: cannot create window: %v", err))
	}
	w.native = nw
	if w.stateKey != "" {
		w.initState(popts)
	}

	windows.Lock()
	windows.list = append(windows.list, w)
	windows.Unlock()

	fire1(&App.onWindowCreated, w)
	if opts.URL != "" {
		w.LoadURL(opts.URL)
	}
	if !opts.Hidden {
		w.Show()
	}
	return w
}

func (w *Window) platformOptions(o *WindowOptions) *platform.WindowOptions {
	p := &platform.WindowOptions{
		Title:          o.Title,
		X:              o.X,
		Y:              o.Y,
		Width:          or(o.Width, 800),
		Height:         or(o.Height, 600),
		Center:         o.X == 0 && o.Y == 0,
		UseContentSize: o.UseContentSize,
		MinSize:        platform.Size{Width: o.MinWidth, Height: o.MinHeight},
		MaxSize:        platform.Size{Width: o.MaxWidth, Height: o.MaxHeight},
		Resizable:      !o.DisableResize,
		Movable:        !o.DisableMove,
		Minimizable:    !o.DisableMinimize,
		Maximizable:    !o.DisableMaximize,
		Closable:       !o.DisableClose,
		Focusable:      true,
		Fullscreenable: !o.DisableFullScreen,
		AlwaysOnTop:    o.AlwaysOnTop,
		FullScreen:     o.FullScreen,
		Maximized:      o.Maximized,
		SkipTaskbar:    o.SkipTaskbar,
		HasShadow:      !o.DisableShadow,
		Frameless:      o.Frameless,
		Transparent:    o.Transparent,
		TitleBarStyle:  string(o.TitleBarStyle),
		Vibrancy:       string(o.Vibrancy),
		Opacity:        o.Opacity,
		Modal:          o.Modal,
		UserAgent:      o.UserAgent,
		Zoom:           o.ZoomFactor,
		Schemes:        Protocol.schemes(),
	}
	if p.Title == "" {
		p.Title = App.Name()
	}
	if p.Opacity <= 0 || p.Opacity > 1 {
		p.Opacity = 1
	}
	if p.Zoom <= 0 {
		p.Zoom = 1
	}
	switch o.DevTools {
	case DevToolsEnabled:
		p.DevTools = true
	case DevToolsAuto:
		p.DevTools = IsDev()
	}
	if o.TrafficLightPosition != nil {
		p.TrafficLightPosition = &platform.Point{X: o.TrafficLightPosition.X, Y: o.TrafficLightPosition.Y}
	}
	if o.Parent != nil && o.Parent.native != nil {
		p.Parent = o.Parent.native
	}
	p.UserScripts = []platform.UserScript{{Source: bridge.Script(bridge.Config{
		Platform: jsPlatform(),
		WindowID: w.id,
		Version:  Version,
		Secret:   w.secret,
	})}}
	if o.PreloadScript != "" {
		p.UserScripts = append(p.UserScripts, platform.UserScript{Source: o.PreloadScript})
	}
	return p
}

func jsPlatform() string {
	if runtime.GOOS == "windows" {
		return "win32"
	}
	return runtime.GOOS
}

func or[T comparable](v, fallback T) T {
	var zero T
	if v == zero {
		return fallback
	}
	return v
}

// Windows returns all open windows in creation order.
func Windows() []*Window {
	windows.Lock()
	defer windows.Unlock()
	return append([]*Window(nil), windows.list...)
}

// FocusedWindow returns the focused window of this application, or nil.
func FocusedWindow() *Window {
	windows.Lock()
	defer windows.Unlock()
	return windows.focused
}

// WindowByID returns the window with the given id, or nil.
func WindowByID(id int) *Window {
	windows.Lock()
	defer windows.Unlock()
	for _, w := range windows.list {
		if w.id == id {
			return w
		}
	}
	return nil
}

// ID returns the unique id of the window. The page sees it as
// window.mygo.windowId.
func (w *Window) ID() int { return w.id }

// IsDestroyed reports whether the window has been closed.
func (w *Window) IsDestroyed() bool { return w.destroyed.Load() }

// Parent returns the parent window, or nil.
func (w *Window) Parent() *Window { return w.parent }

// do runs fn with the native window on the main thread, unless the window
// has been destroyed.
func (w *Window) do(fn func(n platform.Window)) {
	onMain(func() {
		if w.native != nil {
			fn(w.native)
		}
	})
}

// get is do for functions returning a value.
func get[T any](w *Window, fn func(n platform.Window) T) T {
	return onMainValue(func() T {
		if w.native == nil {
			var zero T
			return zero
		}
		return fn(w.native)
	})
}

// Close closes the window as if the user clicked its close button, so
// OnClose listeners can cancel it.
func (w *Window) Close() { onMain(func() { w.close() }) }

// close runs on the main thread and reports whether the window was closed.
func (w *Window) close() bool {
	if w.native == nil {
		return true
	}
	e := &CloseEvent{Window: w}
	fire1(&w.onClose, e)
	if e.prevented {
		return false
	}
	w.destroy()
	return true
}

// Destroy closes the window without emitting OnClose.
func (w *Window) Destroy() { onMain(w.destroy) }

func (w *Window) destroy() {
	if n := w.native; n != nil {
		w.closeState()
		n.Close()
		// Backends report Closed synchronously, but be defensive.
		(&windowHandler{w}).Closed()
	}
}

// Show shows and focuses the window.
func (w *Window) Show() {
	onMain(func() {
		if w.native == nil {
			return
		}
		visible := w.native.IsVisible()
		w.native.Show()
		w.shown = true
		if !visible {
			fire(&w.onShow)
		}
	})
}

// ShowInactive shows the window without focusing it.
func (w *Window) ShowInactive() {
	onMain(func() {
		if w.native == nil {
			return
		}
		visible := w.native.IsVisible()
		w.native.ShowInactive()
		w.shown = true
		if !visible {
			fire(&w.onShow)
		}
	})
}

// Hide hides the window.
func (w *Window) Hide() {
	onMain(func() {
		if w.native == nil || !w.native.IsVisible() {
			return
		}
		w.native.Hide()
		fire(&w.onHide)
	})
}

// IsVisible reports whether the window is shown.
func (w *Window) IsVisible() bool { return get(w, platform.Window.IsVisible) }

// Focus focuses the window.
func (w *Window) Focus() { w.do(platform.Window.Focus) }

// Blur removes focus from the window.
func (w *Window) Blur() { w.do(platform.Window.Blur) }

// IsFocused reports whether the window has keyboard focus.
func (w *Window) IsFocused() bool { return get(w, platform.Window.IsFocused) }

// Minimize minimizes the window.
func (w *Window) Minimize() { w.do(platform.Window.Minimize) }

// IsMinimized reports whether the window is minimized.
func (w *Window) IsMinimized() bool { return get(w, platform.Window.IsMinimized) }

// Maximize maximizes the window.
func (w *Window) Maximize() { w.do(platform.Window.Maximize) }

// Unmaximize restores a maximized window.
func (w *Window) Unmaximize() { w.do(platform.Window.Unmaximize) }

// ToggleMaximize maximizes the window or restores it if it is maximized.
func (w *Window) ToggleMaximize() {
	w.do(func(n platform.Window) {
		if n.IsMaximized() {
			n.Unmaximize()
		} else {
			n.Maximize()
		}
	})
}

// IsMaximized reports whether the window is maximized.
func (w *Window) IsMaximized() bool { return get(w, platform.Window.IsMaximized) }

// Restore restores a minimized window.
func (w *Window) Restore() { w.do(platform.Window.Restore) }

// SetFullScreen enters or leaves full screen.
func (w *Window) SetFullScreen(v bool) {
	w.do(func(n platform.Window) { n.SetFullScreen(v) })
}

// ToggleFullScreen enters full screen or leaves it.
func (w *Window) ToggleFullScreen() {
	w.do(func(n platform.Window) { n.SetFullScreen(!n.IsFullScreen()) })
}

// IsFullScreen reports whether the window is in full screen.
func (w *Window) IsFullScreen() bool { return get(w, platform.Window.IsFullScreen) }

// Center moves the window to the center of its screen.
func (w *Window) Center() { w.do(platform.Window.Center) }

// SetTitle sets the native window title.
func (w *Window) SetTitle(title string) {
	w.do(func(n platform.Window) { n.SetTitle(title) })
}

// Title returns the native window title.
func (w *Window) Title() string { return get(w, platform.Window.Title) }

// SetBounds moves and resizes the window.
func (w *Window) SetBounds(r Rectangle) {
	w.do(func(n platform.Window) { n.SetBounds(platform.Rect(r)) })
}

// Bounds returns the position and size of the window.
func (w *Window) Bounds() Rectangle {
	return get(w, func(n platform.Window) Rectangle { return Rectangle(n.Bounds()) })
}

// SetContentBounds moves and resizes the window so the page area has the
// given bounds.
func (w *Window) SetContentBounds(r Rectangle) {
	w.do(func(n platform.Window) { n.SetContentBounds(platform.Rect(r)) })
}

// ContentBounds returns the bounds of the page area.
func (w *Window) ContentBounds() Rectangle {
	return get(w, func(n platform.Window) Rectangle { return Rectangle(n.ContentBounds()) })
}

// SetSize resizes the window.
func (w *Window) SetSize(width, height int) {
	w.do(func(n platform.Window) {
		b := n.Bounds()
		b.Width, b.Height = width, height
		n.SetBounds(b)
	})
}

// Size returns the size of the window.
func (w *Window) Size() (width, height int) {
	b := w.Bounds()
	return b.Width, b.Height
}

// SetContentSize resizes the window so the page area has the given size.
func (w *Window) SetContentSize(width, height int) {
	w.do(func(n platform.Window) {
		b := n.ContentBounds()
		b.Width, b.Height = width, height
		n.SetContentBounds(b)
	})
}

// ContentSize returns the size of the page area.
func (w *Window) ContentSize() (width, height int) {
	b := w.ContentBounds()
	return b.Width, b.Height
}

// SetPosition moves the window's top-left corner.
func (w *Window) SetPosition(x, y int) {
	w.do(func(n platform.Window) {
		b := n.Bounds()
		b.X, b.Y = x, y
		n.SetBounds(b)
	})
}

// Position returns the window's top-left corner.
func (w *Window) Position() (x, y int) {
	b := w.Bounds()
	return b.X, b.Y
}

// SetMinimumSize limits how small the window can be resized; 0 means no
// limit.
func (w *Window) SetMinimumSize(width, height int) {
	w.do(func(n platform.Window) { n.SetMinimumSize(platform.Size{Width: width, Height: height}) })
}

// SetMaximumSize limits how large the window can be resized; 0 means no
// limit.
func (w *Window) SetMaximumSize(width, height int) {
	w.do(func(n platform.Window) { n.SetMaximumSize(platform.Size{Width: width, Height: height}) })
}

// SetResizable sets whether the user can resize the window.
func (w *Window) SetResizable(v bool) { w.do(func(n platform.Window) { n.SetResizable(v) }) }

// IsResizable reports whether the user can resize the window.
func (w *Window) IsResizable() bool { return get(w, platform.Window.IsResizable) }

// SetMovable sets whether the user can move the window.
func (w *Window) SetMovable(v bool) { w.do(func(n platform.Window) { n.SetMovable(v) }) }

// IsMovable reports whether the user can move the window.
func (w *Window) IsMovable() bool { return get(w, platform.Window.IsMovable) }

// SetMinimizable sets whether the window can be minimized.
func (w *Window) SetMinimizable(v bool) { w.do(func(n platform.Window) { n.SetMinimizable(v) }) }

// IsMinimizable reports whether the window can be minimized.
func (w *Window) IsMinimizable() bool { return get(w, platform.Window.IsMinimizable) }

// SetMaximizable sets whether the window can be maximized.
func (w *Window) SetMaximizable(v bool) { w.do(func(n platform.Window) { n.SetMaximizable(v) }) }

// IsMaximizable reports whether the window can be maximized.
func (w *Window) IsMaximizable() bool { return get(w, platform.Window.IsMaximizable) }

// SetClosable sets whether the user can close the window.
func (w *Window) SetClosable(v bool) { w.do(func(n platform.Window) { n.SetClosable(v) }) }

// IsClosable reports whether the user can close the window.
func (w *Window) IsClosable() bool { return get(w, platform.Window.IsClosable) }

// SetAlwaysOnTop keeps the window above other windows.
func (w *Window) SetAlwaysOnTop(v bool) { w.do(func(n platform.Window) { n.SetAlwaysOnTop(v) }) }

// IsAlwaysOnTop reports whether the window stays above other windows.
func (w *Window) IsAlwaysOnTop() bool { return get(w, platform.Window.IsAlwaysOnTop) }

// SetBackgroundColor sets the color shown behind the page, in the syntax of
// WindowOptions.BackgroundColor.
func (w *Window) SetBackgroundColor(color string) error {
	bg, err := parseBackground(color)
	if err != nil {
		return err
	}
	w.do(func(n platform.Window) {
		w.background = &bg
		n.SetBackgroundColor(*w.backgroundColor())
	})
	return nil
}

// SetOpacity sets the window opacity between 0 and 1.
func (w *Window) SetOpacity(v float64) {
	w.do(func(n platform.Window) { n.SetOpacity(min(max(v, 0), 1)) })
}

// Opacity returns the window opacity.
func (w *Window) Opacity() float64 { return get(w, platform.Window.Opacity) }

// SetHasShadow sets whether the window has a shadow.
func (w *Window) SetHasShadow(v bool) { w.do(func(n platform.Window) { n.SetHasShadow(v) }) }

// HasShadow reports whether the window has a shadow.
func (w *Window) HasShadow() bool { return get(w, platform.Window.HasShadow) }

// SetIgnoreMouseEvents makes the window transparent to mouse events.
func (w *Window) SetIgnoreMouseEvents(v bool) {
	w.do(func(n platform.Window) { n.SetIgnoreMouseEvents(v) })
}

// SetContentProtection keeps the window content out of screenshots and
// screen recordings.
func (w *Window) SetContentProtection(v bool) {
	w.do(func(n platform.Window) { n.SetContentProtection(v) })
}

// SetVibrancy sets the material behind the page; VibrancyNone removes it.
// See WindowOptions.Vibrancy.
func (w *Window) SetVibrancy(v Vibrancy) {
	w.do(func(n platform.Window) { n.SetVibrancy(string(v)) })
}

// ProgressState is the state of a progress bar; see ProgressBar.
type ProgressState string

// Progress states.
const (
	ProgressNone          ProgressState = ""
	ProgressNormal        ProgressState = "normal"
	ProgressIndeterminate ProgressState = "indeterminate"
	// ProgressPaused and ProgressError color the bar yellow and red on
	// Windows, and show it like ProgressNormal elsewhere.
	ProgressPaused ProgressState = "paused"
	ProgressError  ProgressState = "error"
)

// ProgressBar is the progress of a task, shown on the window's taskbar
// button (Windows), on the application's Dock icon (macOS) or on its
// launcher entry (Linux docks that implement the Unity launcher API, such
// as KDE Plasma's and Ubuntu's). The zero value shows no progress.
type ProgressBar struct {
	// State defaults to ProgressNormal when Value is above 0, else to
	// ProgressNone.
	State ProgressState
	// Value is the fraction done, between 0 and 1.
	Value float64
}

// SetProgressBar shows the progress of a task:
//
//	win.SetProgressBar(mygo.ProgressBar{Value: done / total})
//	win.SetProgressBar(mygo.ProgressBar{}) // done: remove it
//
// On macOS and Linux the progress belongs to the application: the window
// that set it last wins.
func (w *Window) SetProgressBar(p ProgressBar) {
	state, value := p.State, min(max(p.Value, 0), 1)
	if state == ProgressNone && p.Value > 0 {
		state = ProgressNormal
	}
	w.do(func(n platform.Window) { n.SetProgressBar(string(state), value) })
}

// FlashFrame draws the user's attention to the window, or stops doing so:
// its taskbar button flashes until it is focused (Windows), it is marked
// urgent (Linux), or the Dock icon bounces (macOS, while the app is not
// active).
func (w *Window) FlashFrame(flash bool) {
	w.do(func(n platform.Window) { n.FlashFrame(flash) })
}

// SetSkipTaskbar hides the window from the taskbar, or shows it there again
// (Linux, Windows). See WindowOptions.SkipTaskbar.
func (w *Window) SetSkipTaskbar(v bool) {
	w.do(func(n platform.Window) { n.SetSkipTaskbar(v) })
}

// SetVisibleOnAllWorkspaces shows the window on every workspace (macOS
// Spaces, Linux virtual desktops), or on the current one only.
func (w *Window) SetVisibleOnAllWorkspaces(v bool) {
	w.do(func(n platform.Window) { n.SetVisibleOnAllWorkspaces(v) })
}

// SetIcon sets the icon of the window, shown in its title bar and taskbar
// button (Linux, Windows), from a PNG image; nil restores the application
// icon. macOS windows show no icon of their own.
func (w *Window) SetIcon(png []byte) error {
	var err error
	w.do(func(n platform.Window) { err = n.SetIcon(png) })
	return err
}

// SetMenu sets the menu bar of this window (Linux, Windows). On macOS the
// menu bar belongs to the application; see Application.SetMenu.
func (w *Window) SetMenu(m *Menu) {
	onMain(func() {
		w.menu = m
		if w.native != nil {
			w.native.SetMenu(m.snapshot())
		}
	})
}

// NativeHandle returns the native window: NSWindow* on macOS, GtkWindow* on
// Linux and HWND on Windows.
func (w *Window) NativeHandle() uintptr { return get(w, platform.Window.Handle) }

// LoadURL navigates the page to url. A URL without a scheme, such as "/" or
// "/settings?tab=1", is a page of the app's frontend: the dev server at
// devUrl during `mygo dev`, the frontend built into the app otherwise (see
// SetFrontend). Besides http(s) URLs, schemes registered with
// Protocol.Handle can be used.
func (w *Window) LoadURL(rawURL string) error {
	resolved, err := resolveURL(rawURL)
	if err != nil {
		return err
	}
	w.do(func(n platform.Window) { n.LoadURL(resolved) })
	return nil
}

// LoadFile loads a local HTML file. Relative paths are resolved against the
// working directory, then against the directory of the executable (and the
// Resources directory of a macOS app bundle).
func (w *Window) LoadFile(path string) error {
	abs, err := resolveFile(path)
	if err != nil {
		return err
	}
	w.do(func(n platform.Window) { n.LoadFile(abs, filepath.Dir(abs)) })
	return nil
}

func resolveFile(path string) (string, error) {
	if filepath.IsAbs(path) {
		if _, err := os.Stat(path); err != nil {
			return "", err
		}
		return path, nil
	}
	candidates := []string{}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, path))
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(dir, path), filepath.Join(dir, "..", "Resources", path))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("mygo: file %q not found", path)
}

// isTrusted reports whether a page at rawURL may call bound methods.
func (w *Window) isTrusted(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	switch {
	case scheme == "about" || scheme == "file" || scheme == frontendScheme:
		return true
	case Protocol.IsHandled(scheme):
		return true
	}
	origin := scheme + "://" + strings.ToLower(u.Host)
	if isDevOrigin(origin) {
		return true
	}
	for _, o := range w.trustedOrigins {
		if o == "*" || strings.EqualFold(strings.TrimSuffix(o, "/"), origin) {
			return true
		}
	}
	if IsDev() && (scheme == "http" || scheme == "https") {
		switch u.Hostname() {
		case "localhost", "127.0.0.1", "::1":
			return true
		}
	}
	return false
}

// LoadHTML loads an HTML string. Relative URLs in it resolve against
// baseURL, which may be empty.
func (w *Window) LoadHTML(html, baseURL string) {
	w.do(func(n platform.Window) { n.LoadHTML(html, baseURL) })
}

// Reload reloads the page.
func (w *Window) Reload() { w.do(func(n platform.Window) { n.Reload(false) }) }

// ReloadIgnoringCache reloads the page bypassing the cache.
func (w *Window) ReloadIgnoringCache() { w.do(func(n platform.Window) { n.Reload(true) }) }

// Stop stops loading the page.
func (w *Window) Stop() { w.do(platform.Window.StopLoading) }

// GoBack navigates back in history.
func (w *Window) GoBack() { w.do(platform.Window.GoBack) }

// GoForward navigates forward in history.
func (w *Window) GoForward() { w.do(platform.Window.GoForward) }

// CanGoBack reports whether there is a previous page in history.
func (w *Window) CanGoBack() bool { return get(w, platform.Window.CanGoBack) }

// CanGoForward reports whether there is a next page in history.
func (w *Window) CanGoForward() bool { return get(w, platform.Window.CanGoForward) }

// URL returns the URL of the current page.
func (w *Window) URL() string { return get(w, platform.Window.URL) }

// IsLoading reports whether the page is still loading.
func (w *Window) IsLoading() bool { return get(w, platform.Window.IsLoading) }

// SetZoomFactor zooms the page; 1 is 100%.
func (w *Window) SetZoomFactor(f float64) {
	if f <= 0 {
		return
	}
	w.do(func(n platform.Window) { n.SetZoom(f) })
}

// ZoomFactor returns the page zoom; 1 is 100%.
func (w *Window) ZoomFactor() float64 { return get(w, platform.Window.Zoom) }

// SetUserAgent overrides the user agent for subsequent requests.
func (w *Window) SetUserAgent(ua string) { w.do(func(n platform.Window) { n.SetUserAgent(ua) }) }

// UserAgent returns the user agent of the page.
func (w *Window) UserAgent() string { return get(w, platform.Window.UserAgent) }

// OpenDevTools opens the web inspector (unless disabled with
// WindowOptions.DevTools).
func (w *Window) OpenDevTools() { w.do(platform.Window.OpenDevTools) }

// CloseDevTools closes the web inspector.
func (w *Window) CloseDevTools() { w.do(platform.Window.CloseDevTools) }

// IsDevToolsOpened reports whether the web inspector is open.
func (w *Window) IsDevToolsOpened() bool { return get(w, platform.Window.IsDevToolsOpened) }

// ToggleDevTools opens or closes the web inspector.
func (w *Window) ToggleDevTools() {
	w.do(func(n platform.Window) {
		if n.IsDevToolsOpened() {
			n.CloseDevTools()
		} else {
			n.OpenDevTools()
		}
	})
}

// Print opens the print dialog for the page.
func (w *Window) Print() { w.do(platform.Window.Print) }

// CapturePage returns a PNG screenshot of the visible page.
func (w *Window) CapturePage() ([]byte, error) {
	type result struct {
		png []byte
		err error
	}
	ch := make(chan result, 1)
	w.do(func(n platform.Window) {
		n.CapturePage(func(png []byte, err error) { deliver(ch, result{png, err}) })
	})
	if w.IsDestroyed() {
		return nil, errDestroyed
	}
	r := await(ch)
	return r.png, r.err
}

var errDestroyed = errors.New("mygo: window has been destroyed")

// Eval evaluates JavaScript in the page and returns its result, decoded
// from JSON. An expression's value is returned, with promises awaited:
//
//	title, err := win.Eval("document.title")
//	data, err := win.Eval("fetch('/data.json').then(r => r.json())")
//
// Statements run as the body of an async function, so use return to produce
// a value:
//
//	n, err := win.Eval("const items = document.querySelectorAll('li'); return items.length")
//
// Eval is not subject to the page's Content Security Policy.
func (w *Window) Eval(code string) (any, error) {
	return w.EvalContext(context.Background(), code)
}

// EvalContext is Eval with a context that can abort the wait.
func (w *Window) EvalContext(ctx context.Context, code string) (any, error) {
	raw, err := w.eval(ctx, code)
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// EvalAs evaluates JavaScript like Window.Eval and decodes the result into
// a value of type T.
//
//	title, err := mygo.EvalAs[string](win, "document.title")
func EvalAs[T any](w *Window, code string) (T, error) {
	var v T
	raw, err := w.eval(context.Background(), code)
	if err != nil || len(raw) == 0 {
		return v, err
	}
	err = json.Unmarshal(raw, &v)
	return v, err
}

// EvalError is returned by Eval when the script throws.
type EvalError struct {
	Message string
}

func (e *EvalError) Error() string { return "mygo: javascript error: " + e.Message }

const (
	// The expression form: compile errors mean code is not an expression.
	evalExpr = "try{return JSON.stringify({ok:true,v:await(\n%s\n)})}catch(e){return JSON.stringify({ok:false,e:String(e&&e.message||e)})}"
	// The statement form runs code as a function body.
	evalBody = "try{return JSON.stringify({ok:true,v:await(async()=>{\n%s\n})()})}catch(e){return JSON.stringify({ok:false,e:String(e&&e.message||e)})}"
)

func (w *Window) eval(ctx context.Context, code string) (jsontext.Value, error) {
	type result struct {
		raw string
		err error
	}
	// Both the cancellation and the page may answer; the first one counts
	// and the other must not block.
	ch := make(chan result, 2)
	stop := context.AfterFunc(ctx, func() { deliver(ch, result{err: ctx.Err()}) })
	defer stop()

	ran := false
	w.do(func(n platform.Window) {
		ran = true
		expr := strings.TrimRight(code, " \t\r\n;")
		n.CallAsyncFunction(fmt.Sprintf(evalExpr, expr), func(res string, err error) {
			if err == nil {
				deliver(ch, result{raw: res})
				return
			}
			if !strings.HasPrefix(err.Error(), "SyntaxError") {
				// It may have run (and then the page navigated or
				// crashed): running it again could run it twice.
				deliver(ch, result{err: err})
				return
			}
			// Not an expression: run it as a function body instead. The
			// first attempt failed to compile, so nothing ran twice.
			onMain(func() {
				if w.native == nil {
					deliver(ch, result{err: errDestroyed})
					return
				}
				w.native.CallAsyncFunction(fmt.Sprintf(evalBody, code), func(res string, err error) {
					deliver(ch, result{raw: res, err: err})
				})
			})
		})
	})
	if !ran {
		return nil, errDestroyed
	}
	r := await(ch)
	if r.err != nil {
		return nil, r.err
	}
	var out struct {
		OK bool           `json:"ok"`
		V  jsontext.Value `json:"v"`
		E  string         `json:"e"`
	}
	if err := json.Unmarshal([]byte(r.raw), &out); err != nil {
		return nil, fmt.Errorf("mygo: unexpected eval result %q: %w", r.raw, err)
	}
	if !out.OK {
		return nil, &EvalError{Message: out.E}
	}
	return out.V, nil
}

// SetWindowOpenHandler decides what happens on window.open() and clicks on
// target="_blank" links. The handler returns the options of the window to
// open, or nil to deny the request. Without a handler, http(s) URLs open in
// the default browser and everything else is denied.
//
// On macOS the new page keeps its relation to the opener (window.opener).
// On Linux it opens as an independent page, because WebKitGTK would share
// the opener's script message routing with a related page.
func (w *Window) SetWindowOpenHandler(fn func(req WindowOpenRequest) *WindowOptions) {
	w.mu.Lock()
	w.openHandler = fn
	w.mu.Unlock()
}

// resetPage starts a new page context; the previous one is canceled.
func (w *Window) resetPage() {
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), callerKey{}, w))
	w.mu.Lock()
	prev := w.pageCancel
	w.pageCtx, w.pageCancel = ctx, cancel
	w.mu.Unlock()
	if prev != nil {
		prev()
	}
}

// pageContext returns the context of the current page. It is canceled when
// the page navigates away or the window is destroyed.
func (w *Window) pageContext() context.Context {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.pageCtx
}

// maxHeldEvents bounds the events kept for a page that never becomes ready
// (e.g. an image or a failed load).
const maxHeldEvents = 1024

// enqueue schedules a message for the page. Messages are batched into a
// single script evaluation per main loop iteration. Events are held until
// the DOM of the page is ready; replies to calls are sent right away since
// the page is waiting for them.
func (w *Window) enqueue(msg []byte, event bool) {
	w.outMu.Lock()
	if event && !w.domReady {
		if len(w.held) == maxHeldEvents {
			w.held = w.held[1:]
		}
		w.held = append(w.held, msg)
		w.outMu.Unlock()
		return
	}
	w.outbox = append(w.outbox, msg)
	schedule := !w.flushing
	w.flushing = true
	w.outMu.Unlock()
	if schedule {
		postMain(w.flush)
	}
}

func (w *Window) flush() {
	w.outMu.Lock()
	msgs := w.outbox
	w.outbox = nil
	w.flushing = false
	w.outMu.Unlock()
	if w.native == nil || len(msgs) == 0 {
		return
	}
	size := 64
	for _, m := range msgs {
		size += len(m) + 1
	}
	js := make([]byte, 0, size)
	js = append(js, "window.__mygo&&__mygo.receive(["...)
	for i, m := range msgs {
		if i > 0 {
			js = append(js, ',')
		}
		js = append(js, m...)
	}
	js = append(js, "])"...)
	w.native.Eval(string(js))
}

// OnClose is called when the window is about to close. Call
// e.PreventDefault to keep it open.
func (w *Window) OnClose(fn func(e *CloseEvent)) (off func()) { return w.onClose.add(fn, false) }

// OnClosed is called after the window has been closed.
func (w *Window) OnClosed(fn func()) (off func()) { return w.onClosed.add(fn, false) }

// OnFocus is called when the window gains focus.
func (w *Window) OnFocus(fn func()) (off func()) { return w.onFocus.add(fn, false) }

// OnBlur is called when the window loses focus.
func (w *Window) OnBlur(fn func()) (off func()) { return w.onBlur.add(fn, false) }

// OnShow is called when the window is shown.
func (w *Window) OnShow(fn func()) (off func()) { return w.onShow.add(fn, false) }

// OnHide is called when the window is hidden.
func (w *Window) OnHide(fn func()) (off func()) { return w.onHide.add(fn, false) }

// OnReadyToShow is called once, when the first page is ready to be
// displayed. Create the window with Hidden and call Show here to avoid a
// visual flash.
func (w *Window) OnReadyToShow(fn func()) (off func()) { return w.onReadyToShow.add(fn, false) }

// OnResize is called after the window was resized.
func (w *Window) OnResize(fn func()) (off func()) { return w.onResize.add(fn, false) }

// OnFileDrop is called when files, for example from Finder or Explorer, are
// dropped on the page, with their paths. The app's own pages also get them,
// through onFileDrop of mygo-runtime.
//
// The page's own drag and drop keeps working: drop events still reach it
// with the File objects, and drags that start in the page are left alone.
// Only where the page does not handle dragged files are they accepted, so
// that dropping them there reaches OnFileDrop instead of replacing the page
// with the file.
func (w *Window) OnFileDrop(fn func(e *FileDropEvent)) (off func()) {
	return w.onFileDrop.add(fn, false)
}

// OnMove is called after the window was moved.
func (w *Window) OnMove(fn func()) (off func()) { return w.onMove.add(fn, false) }

// OnMaximize is called when the window is maximized.
func (w *Window) OnMaximize(fn func()) (off func()) { return w.onMaximize.add(fn, false) }

// OnUnmaximize is called when the window leaves the maximized state.
func (w *Window) OnUnmaximize(fn func()) (off func()) { return w.onUnmaximize.add(fn, false) }

// OnMinimize is called when the window is minimized.
func (w *Window) OnMinimize(fn func()) (off func()) { return w.onMinimize.add(fn, false) }

// OnRestore is called when the window is restored from minimized.
func (w *Window) OnRestore(fn func()) (off func()) { return w.onRestore.add(fn, false) }

// OnEnterFullScreen is called when the window entered full screen.
func (w *Window) OnEnterFullScreen(fn func()) (off func()) {
	return w.onEnterFullScreen.add(fn, false)
}

// OnLeaveFullScreen is called when the window left full screen.
func (w *Window) OnLeaveFullScreen(fn func()) (off func()) {
	return w.onLeaveFullScreen.add(fn, false)
}

// OnPageTitleUpdated is called when the page's <title> changes. Preventing
// the event keeps the native window title.
func (w *Window) OnPageTitleUpdated(fn func(e *TitleEvent)) (off func()) {
	return w.onPageTitleUpdated.add(fn, false)
}

// OnWillNavigate is called before the page navigates to another URL, for
// example after a link click. Preventing the event cancels the navigation.
// It is not called for LoadURL and friends.
func (w *Window) OnWillNavigate(fn func(e *NavigateEvent)) (off func()) {
	return w.onWillNavigate.add(fn, false)
}

// OnDidNavigate is called when a navigation committed and a new page
// started.
func (w *Window) OnDidNavigate(fn func(url string)) (off func()) {
	return w.onDidNavigate.add(fn, false)
}

// OnDOMReady is called when the page's DOM is ready (DOMContentLoaded).
func (w *Window) OnDOMReady(fn func()) (off func()) { return w.onDOMReady.add(fn, false) }

// OnDidFinishLoad is called when the page finished loading.
func (w *Window) OnDidFinishLoad(fn func()) (off func()) { return w.onDidFinishLoad.add(fn, false) }

// OnDidFailLoad is called when the page failed to load.
func (w *Window) OnDidFailLoad(fn func(err *LoadError)) (off func()) {
	return w.onDidFailLoad.add(fn, false)
}

// OnRenderProcessGone is called when the web content process crashed or
// was killed. Call Reload to recover.
func (w *Window) OnRenderProcessGone(fn func(reason string)) (off func()) {
	return w.onRenderGone.add(fn, false)
}

func (w *Window) readyToShow() {
	if w.readyShow {
		return
	}
	w.readyShow = true
	fire(&w.onReadyToShow)
	signalDevReady()
}

// windowHandler receives native window events.
type windowHandler struct{ w *Window }

func (h *windowHandler) ShouldClose() bool {
	e := &CloseEvent{Window: h.w}
	fire1(&h.w.onClose, e)
	if !e.prevented {
		h.w.closeState()
	}
	return !e.prevented
}

func (h *windowHandler) Closed() {
	w := h.w
	if w.native == nil {
		return
	}
	w.native = nil
	w.destroyed.Store(true)

	windows.Lock()
	for i, x := range windows.list {
		if x == w {
			windows.list = append(windows.list[:i:i], windows.list[i+1:]...)
			break
		}
	}
	if windows.focused == w {
		windows.focused = nil
	}
	children := []*Window{}
	for _, x := range windows.list {
		if x.parent == w {
			children = append(children, x)
		}
	}
	windows.Unlock()

	// Children close with their parent, which reports the last window
	// once they are all gone.
	closingParents++
	for _, c := range children {
		c.destroy()
	}
	closingParents--
	w.mu.Lock()
	cancel := w.pageCancel
	w.mu.Unlock()
	cancel()

	fire(&w.onClosed)
	if closingParents == 0 && len(Windows()) == 0 {
		App.lastWindowClosed()
	}
}

// closingParents counts the windows closing their children (main thread
// only).
var closingParents int

func (h *windowHandler) Focused() {
	windows.Lock()
	windows.focused = h.w
	windows.Unlock()
	fire(&h.w.onFocus)
}

func (h *windowHandler) Blurred() {
	windows.Lock()
	if windows.focused == h.w {
		windows.focused = nil
	}
	windows.Unlock()
	fire(&h.w.onBlur)
}

func (h *windowHandler) Resized()           { h.w.stateChanged(); fire(&h.w.onResize) }
func (h *windowHandler) Moved()             { h.w.stateChanged(); fire(&h.w.onMove) }
func (h *windowHandler) Minimized()         { h.w.stateChanged(); fire(&h.w.onMinimize) }
func (h *windowHandler) Restored()          { h.w.stateChanged(); fire(&h.w.onRestore) }
func (h *windowHandler) Maximized()         { h.w.stateChanged(); fire(&h.w.onMaximize) }
func (h *windowHandler) Unmaximized()       { h.w.stateChanged(); fire(&h.w.onUnmaximize) }
func (h *windowHandler) EnteredFullScreen() { h.w.stateChanged(); fire(&h.w.onEnterFullScreen) }
func (h *windowHandler) LeftFullScreen()    { h.w.stateChanged(); fire(&h.w.onLeaveFullScreen) }

func (h *windowHandler) Message(msg string) { h.w.handleMessage(msg) }

func (h *windowHandler) WillNavigate(nav platform.Navigation) bool {
	if !nav.IsMainFrame || nav.IsReload {
		return true
	}
	e := &NavigateEvent{URL: nav.URL, UserInitiated: nav.UserInitiated}
	fire1(&h.w.onWillNavigate, e)
	return !e.prevented
}

func (h *windowHandler) NavigationStarted(string) {}

func (h *windowHandler) NavigationCommitted(url string) {
	h.w.trusted = h.w.isTrusted(url)
	h.w.outMu.Lock()
	h.w.domReady = false
	h.w.outMu.Unlock()
	h.w.resetPage()
	fire1(&h.w.onDidNavigate, url)
}

func (h *windowHandler) LoadFinished() {
	fire(&h.w.onDidFinishLoad)
	// Pages without the bridge (e.g. images) never report DOM ready.
	h.w.readyToShow()
}

func (h *windowHandler) LoadFailed(url string, code int, desc string) {
	fire1(&h.w.onDidFailLoad, &LoadError{URL: url, Code: code, Description: desc})
	signalDevReady()
}

func (h *windowHandler) TitleChanged(title string) {
	if title == "" {
		return
	}
	e := &TitleEvent{Title: title}
	fire1(&h.w.onPageTitleUpdated, e)
	if !e.prevented && h.w.native != nil {
		h.w.native.SetTitle(title)
	}
}

func (h *windowHandler) NewWindow(req platform.NewWindowRequest) platform.Window {
	h.w.mu.Lock()
	handler := h.w.openHandler
	h.w.mu.Unlock()
	r := WindowOpenRequest{URL: req.URL, FrameName: req.FrameName, Width: req.Width, Height: req.Height}
	if handler == nil {
		if u, err := url.Parse(req.URL); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
			go Shell.OpenExternal(req.URL)
		}
		return nil
	}
	opts := handler(r)
	if opts == nil {
		return nil
	}
	o := *opts
	if o.Width == 0 && req.Width > 0 {
		o.Width, o.Height = req.Width, req.Height
	}
	// The webview loads the URL itself.
	o.URL = ""
	bg, err := backgroundOption(o.BackgroundColor)
	if err != nil {
		log.Printf("mygo: window open handler: %v", err)
	}
	child := newWindow(o, bg, req.Native)
	return child.native
}

func (h *windowHandler) ClosedByPage() { h.w.close() }

func (h *windowHandler) RenderProcessGone(reason string) { fire1(&h.w.onRenderGone, reason) }

func (h *windowHandler) SchemeRequest(req *platform.SchemeRequest) {
	Protocol.serve(h.w, req)
}

// handleMessage routes a message posted by the page bridge. It runs on the
// main thread.
func (w *Window) handleMessage(msg string) {
	msg, ok := strings.CutPrefix(msg, w.secret)
	if !ok {
		return // not from the bridge
	}
	// Calls are decoded and executed off the main thread. The page context
	// is captured here so a navigation cannot slip in between.
	if len(msg) > 12 && msg[:12] == `{"t":"call",` {
		go handleCall(w, w.pageContext(), msg, w.trusted)
		return
	}
	var m struct {
		T string  `json:"t"`
		X float64 `json:"x"` // drop
		Y float64 `json:"y"`
	}
	if err := json.Unmarshal([]byte(msg), &m); err != nil {
		return
	}
	switch m.T {
	case "dom-ready":
		w.outMu.Lock()
		w.domReady = true
		w.outbox = append(w.outbox, w.held...)
		w.held = nil
		w.outMu.Unlock()
		w.flush()
		fire(&w.onDOMReady)
		w.readyToShow()
	case "drag":
		if w.native != nil {
			w.native.StartDrag()
		}
	case "dblclick":
		if w.native != nil {
			w.native.TitleBarDoubleClicked()
		}
	case "drop":
		if w.native != nil {
			w.filesDropped(w.native.DroppedFiles(), int(m.X), int(m.Y))
		}
	}
}

// filesDropped tells OnFileDrop listeners, and the app's own pages, about
// dropped files.
func (w *Window) filesDropped(paths []string, x, y int) {
	if len(paths) == 0 {
		return
	}
	fire1(&w.onFileDrop, &FileDropEvent{Paths: paths, X: x, Y: y})
	// Local paths are none of other pages' business.
	if w.trusted {
		if msg, err := encodeEvent(fileDropEvent, FileDropEvent{Paths: paths, X: x, Y: y}); err == nil {
			w.enqueue(msg, true)
		}
	}
}
