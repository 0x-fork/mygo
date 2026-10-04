package ui

import (
	"slices"
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
	preferences() platform.Preferences
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

	// What the last frame laid out, for input until the next one: the
	// focus order, with the scope of each element, and the dialog on top.
	hits        []hit
	focusOrder  []uint64
	focusScopes []focusScope
	modal       uint64
	commitScope focusScope
	// The focus groups of the frame, the group of each element of the
	// focus order in one, and the element of each that had the focus
	// last.
	// clickLater are the elements the view clicked, as a toolbar's
	// overflow menu does, whose clicks the next pass sees.
	clickLater []uint64
	groups     map[uint64]groupInfo
	memberOf   map[uint64]uint64
	groupLast  map[uint64]uint64
	// openers are the elements that had the focus as overlays opened, by
	// overlay.
	openers   map[uint64]uint64
	regs      []shortcutReg
	nextRegs  []shortcutReg
	delivered []shortcutReg

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
	// mods are the modifiers of the last pointer event.
	mods Modifiers

	consumed  bool
	animating bool
	// late is set when lists built elements while laying out.
	late bool
	// revealIDs are the elements to scroll into view once the frame is
	// laid out.
	revealIDs []uint64
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
	// drag is the value being dragged within the window.
	drag *valueDrag
	// kept are the pages of the history that Routers keep, and commitPage
	// the page around the elements being committed.
	kept       map[uint64]bool
	commitPage uint64
	// announcements are the texts for assistive technology to read out
	// (Context.Announce).
	announcements []string
	// dropOver is the element files are dragged over; access is true once
	// assistive technology asked for the content.
	dropOver   uint64
	access     bool
	blinkStart time.Time
	inFrame    bool
	dark       bool
	darkKnown  bool
	// prefs are the desktop's preferences, read once until they change.
	prefs        Preferences
	prefsKnown   bool
	collect      bool
	labels       []labelNode
	tooltipFrame uint64
	tooltipDepth int
	hoverSince   time.Time
	scrollDrag   scrollDrag
	lastPress    struct {
		at     time.Time
		x, y   float32
		id     uint64
		clicks int
	}
}

// scrollDrag is the scroll bar thumb being dragged: where the pointer
// and the offset started, and the size of the content then, which the
// thumb keeps until it is let go, as the rows of a List measured
// meanwhile change it.
type scrollDrag struct {
	st                 *state
	start              float32
	from               float64
	contentW, contentH float64
	horizontal         bool
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
	// overlay marks the registration of an overlay (overlayShortcut).
	overlay bool
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
	t := LightTheme()
	if rt.dark {
		t = DarkTheme()
	}
	t.follow(rt.preferences())
	return t
}

// themeChanged follows a change of the system appearance, or of the
// desktop's preferences.
func (rt *engine) themeChanged() {
	rt.darkKnown, rt.prefsKnown = false, false
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
	if rt.drag != nil {
		// The source's element is this frame's, if it builds one.
		rt.drag.elem = nil
		rt.dragScroll()
	}

	// An event handled while building (a click, an edit) may change what
	// was built before it: build again, so the frame shows the outcome.
	for pass := 0; pass < 3; pass++ {
		rt.pass = pass
		rt.consumed = false
		rt.nextRegs = rt.nextRegs[:0]
		clear(rt.kept)
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
	if h, ok := rt.host.(*headless); ok {
		// For tests, whether assistive technology reads the window or not.
		h.announced = append(h.announced, rt.announcements...)
	}
	rt.announcements = rt.announcements[:0]
	if rt.animating {
		rt.host.requestFrame()
	}
	rt.armTimer()
	rt.showMenu()
}

// endPass forgets the input the pass handled.
func (rt *engine) endPass() {
	rt.forgetInput()
	// Clicks the view gave its elements, which the next pass sees.
	for _, id := range rt.clickLater {
		if s := rt.states[id]; s != nil {
			s.clicks++
			s.clickMods = 0
		}
	}
	rt.clickLater = rt.clickLater[:0]
	rt.menu.chosen = 0
	rt.delivered = rt.delivered[:0]
	// The next pass may not ask again, as when the view cleared what asked.
	for _, e := range rt.c.reveal {
		if !slices.Contains(rt.revealIDs, e.id) {
			rt.revealIDs = append(rt.revealIDs, e.id)
		}
	}
}

// forgetInput forgets the input of the elements the pass built, which
// they handled.
func (rt *engine) forgetInput() {
	for _, s := range rt.states {
		if s.seen != rt.frame || s.pass != rt.pass {
			continue
		}
		s.clicks, s.rightClicks, s.doubleClicks = 0, 0, 0
		s.dragX, s.dragY = 0, 0
		s.changed, s.submitted, s.typing = false, false, false
		s.dropped = nil
		s.droppedValue, s.hasDropped = nil, false
	}
}

// prune forgets the elements the frame did not build, but those of the
// pages Routers keep.
func (rt *engine) prune() {
	for id, s := range rt.states {
		if s.seen != rt.frame || s.pass != rt.pass {
			if rt.pressed == s {
				rt.pressed = nil
			}
			if !rt.keptAlive(s) {
				delete(rt.states, id)
			}
		}
	}
	rt.restoreFocus()
	// The focus does not stay in a page kept out of sight.
	if s := rt.states[rt.focused]; s != nil && (s.seen != rt.frame || s.pass != rt.pass) {
		rt.focused = 0
	}
}

// keptAlive reports whether a state the frame did not build is in a page
// that a Router keeps: one of its history, or inside one.
func (rt *engine) keptAlive(s *state) bool {
	if len(rt.kept) == 0 {
		return false
	}
	for n := 0; s != nil && n < 64; n++ {
		if rt.kept[s.id] {
			return true
		}
		if s.page == 0 {
			return false
		}
		s = rt.states[s.page]
	}
	return false
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
	rt.focusOrder, rt.focusScopes = rt.focusOrder[:0], rt.focusScopes[:0]
	rt.modal, rt.commitScope, rt.commitPage = 0, focusScope{}, 0
	if rt.groups == nil {
		rt.groups = map[uint64]groupInfo{}
	}
	clear(rt.groups)
	rt.labels = rt.labels[:0]
	full := Rect{0, 0, root.w, root.h}
	rt.commitElement(root, full, false)
	rt.arrangeFocus()
	rt.noteGroups()
}

func (rt *engine) commitElement(e *Element, clip Rect, hidden bool) {
	inline := e.isInline()
	if e.kind == kindText && e.first != nil && !inline {
		placeInline(e, e, 0)
	}
	saved, savedPage := rt.enterScope(e), rt.commitPage
	defer func() { rt.commitScope, rt.commitPage = saved, savedPage }()
	s := e.st
	s.page = rt.commitPage
	if e.flags&flagPage != 0 {
		rt.commitPage = e.id
	}
	s.x, s.y, s.w, s.h = e.x, e.y, e.w, e.h
	if e.parent != nil {
		s.parent = e.parent.id
	} else {
		s.parent = 0
	}
	s.flags = e.flags
	if e.parent != nil && e.parent.st.flags&flagDisabled != 0 {
		// Disabled with the element around it, which may have been
		// disabled after building it, as a Fieldset.
		s.flags |= flagDisabled
	}
	s.cursor = e.cursor
	s.role = e.role
	s.input, s.caret, s.takesText = e.inputFn, e.caret, e.takesText
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
	// the focus, and has no text to find; so does what is inert, which
	// shows.
	invisible := e.flags&(flagInvisible|flagInert) != 0 || hidden
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
	label := e.label
	if f := e.nameFrom; label == "" && f != nil {
		label = f.nameOf() // an input, named by its field
	}
	if (label != "" || e.kind == kindText) && !invisible {
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
		rt.focusScopes = append(rt.focusScopes, rt.commitScope)
		// Tab goes to the radio button or tab chosen; not to a toggle
		// that is on.
		if g := rt.commitScope.group; g != 0 && e.checked == 2 && (e.role == RoleRadio || e.role == RoleTab) {
			if info := rt.groups[g]; info.checked == 0 {
				info.checked = e.id
				rt.groups[g] = info
			}
		}
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
