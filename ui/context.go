package ui

import (
	"fmt"
	"hash/maphash"
	"time"

	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/text"
)

// Context builds a window's user interface for one frame. The window's
// view function receives it on the main thread; it is only valid during
// that call.
type Context struct {
	rt       *engine
	parent   *Element
	root     *Element
	chunks   [][]Element
	used     int
	theme    *Theme
	now      time.Time
	w, h     float32
	titleBar TitleBar
	overlay  *Element
	// tree is the Tree being built, for its items.
	tree *treeBuild
}

const chunkSize = 256

// alloc returns a zeroed element from the frame's arena.
func (c *Context) alloc() *Element {
	ci, ei := c.used/chunkSize, c.used%chunkSize
	if ci == len(c.chunks) {
		c.chunks = append(c.chunks, make([]Element, chunkSize))
	}
	c.used++
	e := &c.chunks[ci][ei]
	shadows := e.shadows[:0]
	*e = Element{}
	e.shadows = shadows
	return e
}

func (c *Context) reset(now time.Time, w, h float32) {
	c.used = 0
	c.now = now
	c.w, c.h = w, h
	c.theme = c.rt.defaultTheme()
	c.tree = nil
	root := c.alloc()
	root.c = c
	root.id = 1
	root.kind = kindBox
	root.shrink = 1
	root.align, root.self = alignAuto, alignAuto
	root.width, root.height = px(w), px(h)
	root.st = c.rt.stateFor(root.id)
	root.bg = c.theme.Background
	root.ts = textStyle{set: setColor | setSize | setFamily, color: c.theme.Text, size: c.theme.FontSize, family: c.theme.Font}
	c.root = root
	c.parent = root
	c.overlay = nil
}

// overlayRoot returns the layer above the window's content that Overlay
// builds into; the frame adds it to the root once the view returns.
func (c *Context) overlayRoot() *Element {
	if c.overlay == nil {
		o := c.alloc()
		o.c = c
		o.id = mix(1, 0x6f7665726c6179)
		o.kind = kindBox
		o.shrink = 1
		o.align, o.self = alignAuto, alignAuto
		o.flags = flagAbsolute | flagPassThrough
		o.inset = [4]length{px(0), px(0), px(0), px(0)}
		o.depth = 1
		o.st = c.rt.stateFor(o.id)
		c.overlay = o
	}
	return c.overlay
}

// newElement creates an element of kind as the last child of the current
// parent.
func (c *Context) newElement(k kind) *Element {
	if c.parent == nil {
		panic("ui: element created outside of a view function")
	}
	e := c.alloc()
	e.c = c
	e.kind = k
	e.shrink = 1
	e.align, e.self = alignAuto, alignAuto
	p := c.parent
	e.id = mix(p.id, uint64(p.nchild)+uint64(k)<<56)
	p.add(e)
	e.st = c.rt.stateFor(e.id)
	return e
}

var keySeed = maphash.MakeSeed()

func (c *Context) rekey(e *Element, k any) {
	var h uint64
	switch k := k.(type) {
	case string:
		h = maphash.String(keySeed, k)
	case int:
		h = uint64(k)*0x9E3779B97F4A7C15 + 1
	case int64:
		h = uint64(k)*0x9E3779B97F4A7C15 + 1
	case uint64:
		h = k*0x9E3779B97F4A7C15 + 1
	default:
		h = maphash.String(keySeed, fmt.Sprintf("%T:%v", k, k))
	}
	parent := uint64(0)
	if e.parent != nil {
		parent = e.parent.id
	}
	e.id = mix(parent, h^0xA5A5A5A5A5A5A5A5)
	e.st = c.rt.stateFor(e.id)
}

// mix combines two hashes (the finalizer of splitmix64).
func mix(a, b uint64) uint64 {
	h := a*0x9E3779B97F4A7C15 ^ (b + 0x632BE59BD9B4E019)
	h ^= h >> 30
	h *= 0xBF58476D1CE4E5B9
	h ^= h >> 27
	h *= 0x94D049BB133111EB
	h ^= h >> 31
	return h
}

// Theme returns the theme of the frame: the light or dark theme following
// the system's appearance, unless SetTheme replaced it.
func (c *Context) Theme() *Theme { return c.theme }

// SetTheme makes the frame use t, for the window's root and the widgets
// created after the call.
func (c *Context) SetTheme(t *Theme) {
	c.theme = t
	c.root.bg = t.Background
	c.root.ts.color, c.root.ts.size, c.root.ts.family = t.Text, t.FontSize, t.Font
}

// Size returns the size of the window's content in DIPs.
func (c *Context) Size() (width, height float32) { return c.w, c.h }

// TitleBar is the room the window controls take at the top of a window
// with a hidden title bar, in DIPs: they sit in a band of Height along the
// top edge, Left wide from the left edge and Right wide from the right.
type TitleBar struct{ Height, Left, Right float32 }

// TitleBar returns the room the window controls take in a window with a
// hidden title bar (mygo.WindowOptions.TitleBarStyle), whose view draws
// the title bar under them: the traffic lights on macOS, the window
// buttons on Linux and Windows. It is zero in other windows and in full
// screen, where the controls hide. Keep clear of the controls, and let the
// title bar drag the window:
//
//	bar := c.TitleBar()
//	ui.Row(c).Height(max(bar.Height, 32)).Padding(0, bar.Right, 0, bar.Left).DragWindow()
func (c *Context) TitleBar() TitleBar { return c.titleBar }

// Now returns the time the frame started, for animations.
func (c *Context) Now() time.Time { return c.now }

// Root returns the element holding the window's content: a column the
// size of the window.
func (c *Context) Root() *Element { return c.root }

// Invalidate asks for another frame. It is safe from any goroutine, for
// state that changed outside of the window's events.
func (c *Context) Invalidate() { c.rt.host.invalidate() }

// AnimationFrame asks for another frame as soon as the display can show
// it, for something moving. Call it in every frame while it moves.
func (c *Context) AnimationFrame() { c.rt.animating = true }

// After asks for another frame after d, for something that changes with
// time, such as a clock.
func (c *Context) After(d time.Duration) { c.rt.scheduleAt(c.now.Add(d)) }

// Shortcut reports whether the key with exactly the modifiers mods was
// pressed, wherever the keyboard focus is, unless a focused element
// handled it first.
func (c *Context) Shortcut(mods Modifiers, key Key) bool {
	return c.rt.shortcut(0, mods, key)
}

// Local returns state of type T that element e keeps from frame to frame,
// keyed by key among its states; init creates it the first time. Custom
// widgets keep what they need with it, on the element they create.
func Local[T any](e *Element, key any, init func() T) *T {
	st := e.st
	if st.locals == nil {
		st.locals = map[any]any{}
	}
	if v, ok := st.locals[key]; ok {
		if p, ok := v.(*T); ok {
			return p
		}
	}
	v := new(T)
	*v = init()
	st.locals[key] = v
	return v
}

// state is what the runtime keeps about an element from frame to frame.
type state struct {
	id   uint64
	seen uint64
	pass int    // the pass of the frame that built it last
	born uint64 // the frame that first built the element
	// The element's box and its visible part in the last frame.
	x, y, w, h     float32
	vx, vy, vw, vh float32
	parent         uint64
	flags          uint32
	cursor         Cursor

	clicks, rightClicks, doubleClicks int
	// press is where the pointer went down in the element; dragX and dragY
	// add up its moves since the last frame.
	pressX, pressY float32
	dragX, dragY   float32
	pressed        bool

	scrollX, scrollY float32
	contentW         float32
	contentH         float32
	// cx and cw are the left and width of the element's content box,
	// inside its padding, in the last frame.
	cx, cw float32

	changed, submitted bool
	dropped            []string
	editor             *editor
	locals             map[any]any
	anims              map[any]*anim
	shortcuts          []shortcut
	delivered          []shortcut
}

type shortcut struct {
	mods Modifiers
	key  Key
}

func (rt *engine) stateFor(id uint64) *state {
	s := rt.states[id]
	if s == nil {
		s = &state{id: id, born: rt.frame}
		rt.states[id] = s
	}
	s.seen, s.pass = rt.frame, rt.pass
	return s
}

// textSystem is the shared text system.
func textSystem() *text.System { return text.Shared() }

// sceneColor converts a color for the scene.
func sceneColor(c Color) scene.Color { return c.scene() }
