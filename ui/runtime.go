package ui

import (
	"sync"
	"time"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/text"
)

// host is where a runtime's frames go: a window's surface, or memory for
// headless rendering and tests.
type host interface {
	size() (w, h, scale float32)
	present(s *scene.Scene)
	requestFrame()
	setCursor(Cursor)
	setTextInput(t platform.TextInputState)
	updateAccessibility(tree *platform.AccessTree)
	readClipboard() string
	writeClipboard(string)
	startDrag()
	titleBarDoubleClicked()
	isDark() bool
	titleBar() TitleBar
	// invalidate asks for a frame from any goroutine.
	invalidate()
	openURL(string)
	// popupMenu shows a context menu at (x, y) after the event being
	// handled; chosen receives the ID of the item chosen.
	popupMenu(m *platform.Menu, x, y float32, chosen func(id int))
}

// engine runs the user interface of one window: it builds frames with
// the view function, lays them out, paints them and routes input to the
// elements of the last frame. Main thread only, except where noted.
type engine struct {
	view  func(*Context)
	host  host
	c     Context
	text  *text.System
	scene scene.Scene
	paths paths
	svgs  svgs
	flex  flexScratch
	grid  gridScratch

	states map[uint64]*state
	frame  uint64
	// pass is the pass of the view building the frame: the last one
	// builds the elements that stay.
	pass int

	// What the last frame laid out, for input until the next one.
	hits       []hit
	focusOrder []uint64
	regs       []shortcutReg
	nextRegs   []shortcutReg
	delivered  []shortcutReg

	pointerX, pointerY float32
	pointerIn          bool
	hover              []uint64
	pressed            *state
	pressButton        int
	focused            uint64
	focusVisible       bool
	windowFocused      bool
	keys               []keyEvent
	menu               menuState
	toasts             []toast
	nextToast          uint64

	consumed  bool
	animating bool
	wakeMu    sync.Mutex
	wakeAt    time.Time
	timer     *time.Timer
	cursor    Cursor
	// ime is the text input state the host has, and base the rune of the
	// focused editor its text starts at.
	ime struct {
		state platform.TextInputState
		base  int
	}
	// dropOver is the element files are dragged over; access is true once
	// assistive technology asked for the content.
	dropOver     uint64
	access       bool
	blinkStart   time.Time
	inFrame      bool
	dark         bool
	darkKnown    bool
	collect      bool
	labels       []labelNode
	tooltipFrame uint64
	tooltipDepth int
	hoverSince   time.Time
	scrollDrag   struct {
		st          *state
		start, from float32
		horizontal  bool
	}
	lastPress struct {
		at     time.Time
		x, y   float32
		id     uint64
		clicks int
	}
}

// labelNode is an element showing or labeled with text, for tests and
// assistive technology.
type labelNode struct {
	id   uint64
	text string
	r    Rect
}

type hit struct {
	st    *state
	r     Rect
	flags uint32
}

type shortcutReg struct {
	id   uint64
	mods Modifiers
	key  Key
}

type keyEvent struct {
	mods Modifiers
	key  Key
}

func newRuntime(view func(*Context), h host) *engine {
	rt := &engine{view: view, host: h, text: textSystem(), states: map[uint64]*state{}, windowFocused: true}
	rt.c.rt = rt
	return rt
}

func (rt *engine) defaultTheme() *Theme {
	if !rt.darkKnown {
		rt.dark, rt.darkKnown = rt.host.isDark(), true
	}
	if rt.dark {
		return DarkTheme()
	}
	return LightTheme()
}

// themeChanged follows a change of the system appearance.
func (rt *engine) themeChanged() {
	rt.darkKnown = false
	rt.host.requestFrame()
}

// runFrame builds, lays out, paints and presents a frame.
func (rt *engine) runFrame() {
	if rt.inFrame {
		return
	}
	rt.inFrame = true
	defer func() { rt.inFrame = false }()

	rt.frame++
	now := time.Now()
	w, h, scale := rt.host.size()
	rt.c.titleBar = rt.host.titleBar()
	rt.text.BeginFrame()
	rt.animating = false
	rt.routeKeys()

	// An event handled while building (a click, an edit) may change what
	// was built before it: build again, so the frame shows the outcome.
	for pass := 0; pass < 3; pass++ {
		rt.pass = pass
		rt.consumed = false
		rt.nextRegs = rt.nextRegs[:0]
		rt.c.reset(now, w, h)
		rt.view(&rt.c)
		rt.buildToasts(&rt.c)
		if ov := rt.c.overlay; ov != nil {
			rt.c.root.add(ov)
		}
		rt.resolveMenu()
		rt.endPass()
		if !rt.consumed {
			break
		}
	}
	root := rt.c.root
	layoutTree(root, w, h)
	rt.commit(root)
	rt.paint(root, w, h, scale)
	for try := 0; try < 2 && rt.text.Full(); try++ {
		// The glyph atlas filled up and left some out: make room, keeping
		// what the frame draws, and paint it again.
		rt.text.MakeRoom()
		rt.paint(root, w, h, scale)
	}
	rt.host.present(&rt.scene)
	rt.prune()
	rt.prunePictures()
	rt.text.EndFrame()
	rt.regs, rt.nextRegs = rt.nextRegs, rt.regs
	rt.updateTextInput()
	rt.updateCursor()
	if rt.access {
		rt.host.updateAccessibility(rt.accessTree())
	}
	if rt.animating {
		rt.host.requestFrame()
	}
	rt.armTimer()
	rt.showMenu()
}

// endPass forgets the input the pass handled.
func (rt *engine) endPass() {
	for _, s := range rt.states {
		if s.seen != rt.frame || s.pass != rt.pass {
			continue
		}
		s.clicks, s.rightClicks, s.doubleClicks = 0, 0, 0
		s.dragX, s.dragY = 0, 0
		s.changed, s.submitted = false, false
		s.dropped = nil
	}
	rt.menu.chosen = 0
	rt.delivered = rt.delivered[:0]
}

// prune forgets the elements the frame did not build.
func (rt *engine) prune() {
	for id, s := range rt.states {
		if s.seen != rt.frame || s.pass != rt.pass {
			if rt.pressed == s {
				rt.pressed = nil
			}
			delete(rt.states, id)
		}
	}
}

// requestFrame asks the host for a frame, unless one is being built.
func (rt *engine) requestFrame() {
	if rt.inFrame {
		return
	}
	rt.host.requestFrame()
}

// scheduleAt asks for a frame at t (After).
func (rt *engine) scheduleAt(t time.Time) {
	rt.wakeMu.Lock()
	if rt.wakeAt.IsZero() || t.Before(rt.wakeAt) {
		rt.wakeAt = t
	}
	rt.wakeMu.Unlock()
}

// armTimer starts a timer for the earliest frame After asked for.
func (rt *engine) armTimer() {
	rt.wakeMu.Lock()
	at := rt.wakeAt
	rt.wakeAt = time.Time{}
	rt.wakeMu.Unlock()
	if rt.timer != nil {
		rt.timer.Stop()
		rt.timer = nil
	}
	if at.IsZero() {
		return
	}
	rt.timer = time.AfterFunc(max(time.Until(at), time.Millisecond), rt.host.invalidate)
}

func (rt *engine) close() {
	if rt.timer != nil {
		rt.timer.Stop()
	}
}

// commit records the laid out frame in the elements' states: their
// boxes, the hit list in paint order, the focus order.
func (rt *engine) commit(root *Element) {
	rt.hits = rt.hits[:0]
	rt.focusOrder = rt.focusOrder[:0]
	rt.labels = rt.labels[:0]
	full := Rect{0, 0, root.w, root.h}
	rt.commitElement(root, full, false)
}

func (rt *engine) commitElement(e *Element, clip Rect, hidden bool) {
	inline := e.isInline()
	if e.kind == kindText && e.first != nil && !inline {
		placeInline(e, e, 0)
	}
	s := e.st
	s.x, s.y, s.w, s.h = e.x, e.y, e.w, e.h
	if e.parent != nil {
		s.parent = e.parent.id
	} else {
		s.parent = 0
	}
	s.flags = e.flags
	s.cursor = e.cursor
	if e.flags&(flagEditable|flagSelectable) != 0 && s.cursor == 0 {
		s.cursor = CursorText + 1
	}
	v := intersect(Rect{e.x, e.y, e.w, e.h}, clip)
	s.vx, s.vy, s.vw, s.vh = v.X, v.Y, v.W, v.H
	s.cx, s.cw = e.x+e.contentX(), max(e.w-e.padX(), 0)
	if e.flags&(flagScrollX|flagScrollY) != 0 {
		s.contentW, s.contentH = e.contentW, e.contentH
	}
	// What is invisible keeps its box but takes neither the pointer nor
	// the focus, and has no text to find.
	invisible := e.flags&flagInvisible != 0 || hidden
	if invisible {
		s.vw, s.vh = 0, 0
		if rt.focused == e.id {
			rt.focused = 0
		}
	}
	switch {
	case e.flags&flagPassThrough != 0 || invisible:
	case inline:
		// Inline elements take the pointer over their words.
		for _, r := range e.frags {
			rt.hits = append(rt.hits, hit{s, intersect(r, clip), e.flags})
		}
	default:
		rt.hits = append(rt.hits, hit{s, v, e.flags})
	}
	if label := e.label; (label != "" || e.kind == kindText) && !invisible {
		if label == "" {
			label = e.text
		}
		switch {
		case !inline:
			rt.labels = append(rt.labels, labelNode{e.id, label, v})
		case len(e.frags) > 0 && (e.label != "" || e.flags&interactive != 0):
			// Their paragraph shows the text of the others.
			rt.labels = append(rt.labels, labelNode{e.id, label, intersect(e.frags[0], clip)})
		}
	}
	if e.flags&flagFocusable != 0 && !e.IsDisabled() && !invisible {
		rt.focusOrder = append(rt.focusOrder, e.id)
	}
	if e.flags&(flagClipX|flagClipY|flagScrollX|flagScrollY) != 0 {
		r, _ := e.clipRect()
		clip = intersect(clip, r)
	}
	// Children in flow first, absolute ones above them: the paint order.
	for ch := e.first; ch != nil; ch = ch.next {
		if ch.flags&flagAbsolute == 0 {
			rt.commitElement(ch, clip, invisible)
		}
	}
	for ch := e.first; ch != nil; ch = ch.next {
		if ch.flags&flagAbsolute != 0 {
			rt.commitElement(ch, clip, invisible)
		}
	}
}

func intersect(a, b Rect) Rect {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.W, b.X+b.W), min(a.Y+a.H, b.Y+b.H)
	if x1 <= x0 || y1 <= y0 {
		return Rect{x0, y0, 0, 0}
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// updateCursor shows the cursor of the element under the pointer.
func (rt *engine) updateCursor() {
	c := CursorDefault
	if rt.pressed != nil && rt.pressed.cursor != 0 {
		c = rt.pressed.cursor - 1
	} else {
		for _, id := range rt.hover {
			if s := rt.states[id]; s != nil && s.cursor != 0 {
				c = s.cursor - 1
				break
			}
		}
	}
	if c != rt.cursor {
		rt.cursor = c
		rt.host.setCursor(c)
	}
}
