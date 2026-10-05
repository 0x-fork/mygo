package ui

import (
	"image"
	"log"
	"os"
	"time"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/raster"
	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/surface"
	"github.com/egoist/mygo/internal/text"
)

// Content is a user interface for a window, the value of
// mygo.WindowOptions.Content. Create it with View.
type Content struct {
	view func(*Context)
}

// View returns the content of a window whose user interface view builds,
// for mygo.WindowOptions.Content:
//
//	mygo.NewWindow(mygo.WindowOptions{Title: "Counter", Content: ui.View(app.View)})
//
// view runs on the main thread whenever the window needs a frame: after
// input, after Context.Invalidate or Window.Invalidate, and while
// something animates. A Content can serve several windows, each with its
// own state.
func View(view func(c *Context)) *Content { return &Content{view: view} }

// RegisterFont adds a TrueType or OpenType font, or collection, that text
// can use with Font(family). An empty family keeps the font's own name.
func RegisterFont(data []byte, family string) error {
	return text.Shared().RegisterFont(data, family)
}

// AttachContent connects the content to a window; package mygo calls it.
func (v *Content) AttachContent(conn *surface.Conn) {
	h := &windowHost{conn: conn}
	rt := newRuntime(v.view, h)
	h.rt = rt
	conn.Event = h.event
	conn.ThemeChanged = func() {
		// The interface font is part of the appearance.
		h.uiFont()
		rt.themeChanged()
	}
	conn.TitleBarChanged = rt.requestFrame
	conn.Capture = h.capture
	conn.Detach = h.detach
	rt.insp.enabled = conn.DevTools
	conn.ToggleDevTools = func() {
		if rt.insp.enabled {
			rt.toggleInspector()
		}
	}
	h.uiFont()
	// Load the fonts while the window shows up.
	go text.Shared().Preload()
	conn.Surface.RequestFrame()
}

// windowHost presents frames on a window's surface, with a GPU renderer
// when the platform has one and in memory otherwise.
type windowHost struct {
	conn *surface.Conn
	rt   *engine
	gpu  gpuRenderer
	// gpuTried tells that the first frame tried the GPU, onGPU that its
	// renderer drew on a GPU, degraded that one drawing in software took
	// its place, and gpuSince when the renderer was made. After a failure,
	// retryAt is when a frame makes another, and backoff how long the next
	// failure waits.
	gpuTried bool
	onGPU    bool
	degraded bool
	gpuSince time.Time
	retryAt  time.Time
	backoff  time.Duration
	soft     raster.Renderer
	last     *scene.Scene
	// lastFrame is when the last frame was presented, and gpuSinceCPU
	// when the first since the CPU drew one, if the GPU drew it.
	lastFrame, gpuSinceCPU time.Time
	// framing tells that the surface asked for the frame being drawn.
	framing bool
	// path is how the last frame was drawn, for MYGO_FRAME_STATS.
	path string
}

func (h *windowHost) framePath() string { return h.path }

// newGPU makes the GPU renderer of a surface; tests replace it.
var newGPU = newGPURenderer

// event handles an event of the surface, noting when it asks for a frame:
// only then may renderers draw (OpenGL's context is current only then).
func (h *windowHost) event(ev platform.SurfaceEvent) bool {
	if ev.Kind == platform.SurfaceFrame {
		h.framing = true
		defer func() { h.framing = false }()
	}
	return h.rt.event(ev)
}

// gpuRenderer draws scenes into a surface on the GPU.
type gpuRenderer interface {
	Render(s *scene.Scene) error
	Release()
}

// pixelPresenter is a GPU renderer that also shows frames drawn in memory,
// without the GPU, copying where they changed (Metal's).
type pixelPresenter interface {
	PresentPixels(pix []byte, stride, width, height int, scale float64, damage []image.Rectangle) error
}

// Frames that change little draw on the CPU, which the renderer copies
// into its drawables: a clock ticking, typing, the pointer over a button.
// The CPU draws them in less time than the GPU takes to start, and spares
// the memory Metal's driver holds for a couple of seconds after each frame
// it draws. Frames changing more than a sixteenth of the window while
// frames follow each other, as when scrolling or animating most of it,
// draw on the GPU, as do frames redrawing more than cpuMaxPixels.
const (
	burstGap     = 50 * time.Millisecond // frames closer follow each other
	cpuMaxPixels = 8 << 20
)

func (h *windowHost) refreshRate() float32 { return float32(h.conn.Surface.RefreshRate()) }

func (h *windowHost) size() (float32, float32, float32) {
	w, ht, s := h.conn.Surface.Size()
	if s <= 0 {
		s = 1
	}
	return float32(w), float32(ht), float32(s)
}

func (h *windowHost) present(s *scene.Scene) {
	h.last, h.path = s, ""
	if s.Width <= 0 || s.Height <= 0 {
		return
	}
	if !h.framing {
		// A frame built outside the surface's, for a capture, is shown by
		// the surface's next.
		h.conn.Surface.RequestFrame()
		return
	}
	due := !h.retryAt.IsZero() && !time.Now().Before(h.retryAt)
	switch {
	case !h.gpuTried || h.gpu == nil && due:
		h.makeGPU()
	case due && h.degraded:
		// Drawing in software since the GPU went away: is it back?
		h.gpu.Release()
		h.gpu = nil
		h.makeGPU()
	}
	now := time.Now()
	burst := now.Sub(h.lastFrame) < burstGap
	h.lastFrame = now
	if h.drawOnCPU(s, burst) {
		h.gpuSinceCPU = time.Time{}
		h.path = "drawn on the CPU"
		return
	}
	if h.render(s) {
		h.dropCPUFrame(now)
		h.path = "drawn on the GPU"
		if h.degraded {
			h.path = "drawn by the GPU renderer in software"
		}
		return
	}
	h.path = "drawn in memory"
	h.soft.Render(s)
	m := &h.soft.Image
	h.conn.Surface.PresentPixels(m.Pix, m.Stride, m.W, m.H)
}

// drawOnCPU draws s on the CPU and has the GPU renderer present it, when
// it changes little (see pixelPresenter), and reports whether it did.
func (h *windowHost) drawOnCPU(s *scene.Scene, burst bool) bool {
	p, ok := h.gpu.(pixelPresenter)
	if !ok {
		return false
	}
	changed := h.soft.Changes(s)
	if changed > cpuMaxPixels || burst && changed > s.Width*s.Height/16 {
		return false
	}
	damage := h.soft.Render(s)
	m := &h.soft.Image
	if err := p.PresentPixels(m.Pix, m.Stride, m.W, m.H, float64(s.Scale), damage); err != nil {
		log.Printf("mygo: presenting a frame drawn in memory: %v", err)
		return false
	}
	return true
}

// dropCPUFrame frees the frame the CPU drew last once the GPU has drawn
// alone for a second, as while it animates much of the window: the CPU
// would draw the next frame whole anyway.
func (h *windowHost) dropCPUFrame(now time.Time) {
	if _, ok := h.gpu.(pixelPresenter); !ok {
		return
	}
	if h.gpuSinceCPU.IsZero() {
		h.gpuSinceCPU = now
	} else if now.Sub(h.gpuSinceCPU) > time.Second && h.soft.Image.Pix != nil {
		h.soft.Release()
	}
}

// render draws s with the GPU renderer and reports whether it did. One
// that fails gives way to another made at once, in case the GPU is back
// already, or another, or Windows' software rasterizer, can draw; when
// that fails too, frames are drawn in memory until a later try.
func (h *windowHost) render(s *scene.Scene) bool {
	for try := 0; h.gpu != nil; try++ {
		err := h.gpu.Render(s)
		if err == nil {
			return true
		}
		log.Printf("mygo: drawing without the GPU: %v", err)
		h.gpu.Release()
		h.gpu = nil
		if try > 0 {
			h.retryLater()
			return false
		}
		if time.Since(h.gpuSince) > time.Minute {
			h.backoff = 0 // it worked for a while: the waits start over
		}
		h.makeGPU()
	}
	return false
}

// makeGPU makes the surface's GPU renderer: for the first frame, after one
// failed, and when the wait after a failure is over. Where the first frame
// cannot have one, the window draws in memory for good, and where it has a
// GPU, a renderer that draws in software in its place tries for the GPU
// again from time to time.
func (h *windowHost) makeGPU() {
	retry := h.gpuTried
	h.gpuTried, h.retryAt = true, time.Time{}
	n := h.conn.Surface.Native()
	if os.Getenv("MYGO_GPU") == "0" || n == (platform.SurfaceNative{}) {
		return
	}
	r, err := newGPU(n)
	switch {
	case err != nil:
		log.Printf("mygo: drawing without the GPU: %v", err)
		if retry {
			h.retryLater()
		}
		return
	case r == nil:
		return
	case !retry:
		h.onGPU = !software(r)
	case h.onGPU && software(r):
		if !h.degraded {
			log.Print("mygo: drawing in software until the GPU is back")
		}
		h.degraded = true
		h.retryLater()
	default:
		if !software(r) {
			log.Print("mygo: drawing with the GPU again")
		}
		h.degraded = false
	}
	h.gpu, h.gpuSince = r, time.Now()
}

// software reports whether a renderer draws on the CPU, as Direct3D's WARP.
func software(r gpuRenderer) bool {
	s, ok := r.(interface{ Software() bool })
	return ok && s.Software()
}

// retryLater has a frame make a GPU renderer again after a wait, which
// doubles with each failure up to half a minute: a driver that resets, a
// GPU unplugged or a machine waking up may take a while. A window that
// draws nothing meanwhile is not woken for it.
func (h *windowHost) retryLater() {
	h.backoff = min(max(2*h.backoff, time.Second), 30*time.Second)
	h.retryAt = time.Now().Add(h.backoff)
}

// capture renders the last frame in memory.
func (h *windowHost) capture() (int, int, []byte) {
	if h.last == nil {
		h.rt.runFrame()
	}
	s := h.last
	if s == nil {
		return 0, 0, nil
	}
	var img raster.Image
	img.Resize(s.Width, s.Height)
	raster.Render(&img, s)
	return img.W, img.H, img.RGBA()
}

func (h *windowHost) detach() {
	h.rt.close()
	h.soft.Release()
	if h.gpu != nil {
		h.gpu.Release()
		h.gpu = nil
	}
}

func (h *windowHost) requestFrame() { h.conn.Surface.RequestFrame() }

// uiFont gives system-ui the desktop's interface font, and text the
// desktop's settings for rasterizing it, where the text system does not
// know them.
func (h *windowHost) uiFont() {
	if h.conn.UIFont != nil {
		text.Shared().SetUIFamily(h.conn.UIFont())
	}
	if h.conn.FontRendering != nil {
		r := h.conn.FontRendering()
		text.Shared().SetFontRendering(r.Antialias, r.Hinting, r.Subpixels)
	}
}

func (h *windowHost) setCursor(c Cursor) { h.conn.Surface.SetCursor(platform.Cursor(c)) }

func (h *windowHost) setTextInput(t platform.TextInputState) { h.conn.Surface.SetTextInput(t) }

func (h *windowHost) updateAccessibility(tree *platform.AccessTree) {
	h.conn.Surface.UpdateAccessibility(tree)
}

func (h *windowHost) readClipboard() string {
	if h.conn.Clipboard == nil {
		return ""
	}
	return h.conn.Clipboard.ReadText()
}

func (h *windowHost) writeClipboard(s string) {
	if h.conn.Clipboard != nil {
		h.conn.Clipboard.WriteText(s)
	}
}

func (h *windowHost) startDrag() {
	if h.conn.StartDrag != nil {
		h.conn.StartDrag()
	}
}

func (h *windowHost) titleBarDoubleClicked() {
	if h.conn.TitleBarDoubleClicked != nil {
		h.conn.TitleBarDoubleClicked()
	}
}

func (h *windowHost) isDark() bool { return h.conn.IsDark != nil && h.conn.IsDark() }

func (h *windowHost) preferences() platform.Preferences {
	if h.conn.Preferences == nil {
		return platform.Preferences{}
	}
	return h.conn.Preferences()
}

func (h *windowHost) titleBar() TitleBar {
	if h.conn.TitleBar == nil {
		return TitleBar{}
	}
	t := h.conn.TitleBar()
	return TitleBar{Height: float32(t.Height), Left: float32(t.Left), Right: float32(t.Right)}
}

func (h *windowHost) invalidate() { h.conn.Invalidate() }

func (h *windowHost) openURL(u string) {
	if h.conn.OpenURL != nil {
		h.conn.OpenURL(u)
	}
}

func (h *windowHost) popupMenu(m *platform.Menu, x, y float32, chosen func(int)) {
	if h.conn.PopupMenu != nil {
		h.conn.PopupMenu(m, float64(x), float64(y), chosen)
	}
}
