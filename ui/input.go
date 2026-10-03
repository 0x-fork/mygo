package ui

import (
	"runtime"
	"slices"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// event handles a surface event on the main thread. It reports whether
// an element takes files dragged over or dropped at the event's position.
func (rt *engine) event(ev platform.SurfaceEvent) (taken bool) {
	x, y := float32(ev.X), float32(ev.Y)
	switch ev.Kind {
	case platform.SurfaceFrame:
		rt.runFrame()
	case platform.SurfaceResize:
		rt.requestFrame()
	case platform.PointerMove:
		rt.pointerMove(x, y)
	case platform.PointerDown:
		rt.pointerMove(x, y)
		rt.pointerDown(x, y, ev.Button, Modifiers(ev.Mods))
	case platform.PointerUp:
		rt.pointerMove(x, y)
		rt.pointerUp(ev.Button)
	case platform.PointerLeave:
		rt.pointerIn = false
		if rt.pressed == nil {
			rt.setHover(nil)
		}
	case platform.PointerScroll:
		rt.pointerMove(x, y)
		rt.scroll(float32(ev.DX), float32(ev.DY), Modifiers(ev.Mods))
	case platform.KeyPressed:
		rt.keyDown(Modifiers(ev.Mods), Key(ev.Key))
	case platform.TextInput:
		rt.editEvent(rt.replaced(editEvent{kind: editInsert, text: ev.Text}, ev))
	case platform.TextComposition:
		rt.editEvent(rt.replaced(editEvent{kind: editCompose, text: ev.Text, caret: ev.Caret}, ev))
	case platform.SurfaceCommand:
		rt.editEvent(editEvent{kind: editCommand, text: ev.Text})
	case platform.SurfaceFocus:
		rt.windowFocused = true
		rt.blinkStart = time.Now()
		rt.requestFrame()
	case platform.SurfaceBlur:
		rt.windowFocused = false
		if rt.pressed != nil {
			rt.pressed.pressed = false
			rt.pressed = nil
		}
		rt.requestFrame()
	case platform.FileDragOver:
		taken = rt.fileDrag(x, y)
	case platform.FileDragLeave:
		rt.fileDrag(-1, -1)
	case platform.FileDrop:
		taken = rt.fileDrop(x, y, ev.Files)
	case platform.AccessibilityOn:
		rt.accessibilityOn()
	case platform.AccessAction:
		rt.accessAction(ev)
	}
	if ev.Kind != platform.SurfaceFrame {
		// A press or Tab may have moved the focus: tell the host now, not
		// after the next frame, or the text of keys typed before it would
		// never reach the input method.
		rt.updateTextInput()
	}
	return taken
}

// hitChain returns the ids of the topmost element at (x, y) and of its
// ancestors, innermost first.
func (rt *engine) hitChain(x, y float32) []uint64 {
	for i := len(rt.hits) - 1; i >= 0; i-- {
		h := &rt.hits[i]
		if !h.r.Contains(x, y) {
			continue
		}
		var chain []uint64
		for s := h.st; s != nil; s = rt.states[s.parent] {
			chain = append(chain, s.id)
			if s.parent == 0 {
				break
			}
		}
		return chain
	}
	return nil
}

func (rt *engine) setHover(chain []uint64) {
	changed := len(chain) != len(rt.hover)
	if !changed {
		for i := range chain {
			if chain[i] != rt.hover[i] {
				changed = true
				break
			}
		}
	}
	if !changed {
		return
	}
	// Only elements that look at their hover need a frame.
	need := false
	for _, list := range [][]uint64{chain, rt.hover} {
		for _, id := range list {
			if s := rt.states[id]; s != nil && s.flags&(flagHover|flagTrackPointer) != 0 {
				need = true
			}
		}
	}
	rt.hover = chain
	rt.hoverSince = time.Now()
	if need {
		rt.requestFrame()
	}
	rt.updateCursor()
}

func (rt *engine) pointerMove(x, y float32) {
	if d := &rt.scrollDrag; d.st != nil {
		s := d.st
		g := d.bars(rt.c.theme.scrollbarWidth())
		if d.horizontal {
			if travel := g.hTrack.W - 4 - g.h.W; travel > 0 {
				s.scrollTo(dragTo(d.from, x-d.start, travel, d.contentW-float64(s.w), s.contentW-float64(s.w)), s.scrollY)
			}
		} else if travel := g.vTrack.H - 4 - g.v.H; travel > 0 {
			s.scrollTo(s.scrollX, dragTo(d.from, y-d.start, travel, d.contentH-float64(s.h), s.contentH-float64(s.h)))
		}
		rt.pointerX, rt.pointerY = x, y
		rt.requestFrame()
		return
	}
	if rt.pressed != nil && rt.pointerIn {
		rt.pressed.dragX += x - rt.pointerX
		rt.pressed.dragY += y - rt.pointerY
		if rt.pressed.flags&(flagTrackPointer|flagDraggable|flagEditable|flagSelectable) != 0 {
			rt.requestFrame()
		}
	}
	moved := x != rt.pointerX || y != rt.pointerY
	rt.pointerX, rt.pointerY, rt.pointerIn = x, y, true
	if rt.pressed == nil {
		rt.setHover(rt.hitChain(x, y))
	}
	if moved {
		for _, id := range rt.hover {
			if s := rt.states[id]; s != nil && s.flags&flagTrackPointer != 0 {
				rt.requestFrame()
				break
			}
		}
	}
}

const interactive = flagClickable | flagFocusable | flagEditable | flagSelectable | flagDragWindow | flagDraggable | flagTrackPointer

func (rt *engine) pointerDown(x, y float32, button int, mods Modifiers) {
	chain := rt.hitChain(x, y)
	rt.setHover(chain)
	if button == 0 && rt.scrollbarPress(chain, x, y) {
		return
	}
	if button == 1 && rt.menuPress(chain, x, y) {
		return
	}
	var target, focus *state
	for _, id := range chain {
		s := rt.states[id]
		if s == nil {
			continue
		}
		if target == nil && s.flags&interactive != 0 {
			target = s
		}
		if focus == nil && s.flags&(flagFocusable|flagEditable|flagSelectable) != 0 && s.flags&flagDisabled == 0 {
			focus = s
		}
	}
	if button == 0 {
		newFocus := uint64(0)
		if focus != nil {
			newFocus = focus.id
		}
		if newFocus != rt.focused {
			rt.focused = newFocus
			rt.focusVisible = false
			rt.blinkStart = time.Now()
		}
	}
	rt.requestFrame()
	if target == nil {
		return
	}
	// Count quick successive presses at the same place.
	now := time.Now()
	clicks := 1
	lp := &rt.lastPress
	if lp.id == target.id && now.Sub(lp.at) < 500*time.Millisecond && abs32(x-lp.x) < 5 && abs32(y-lp.y) < 5 {
		clicks = lp.clicks + 1
	}
	lp.at, lp.x, lp.y, lp.id, lp.clicks = now, x, y, target.id, clicks
	if button == 0 && target.flags&flagDragWindow != 0 && target.flags&(interactive&^flagDragWindow) == 0 {
		if clicks == 2 {
			rt.host.titleBarDoubleClicked()
		} else {
			rt.host.startDrag()
		}
		return
	}
	rt.pressed, rt.pressButton = target, button
	target.pressed = true
	target.pressX, target.pressY = x-target.x, y-target.y
	if target.editor != nil {
		target.editor.pressMods = mods
		target.editor.press(x-target.x, y-target.y, clicks, button)
	}
	if clicks == 2 && button == 0 {
		target.doubleClicks++
	}
}

func (rt *engine) pointerUp(button int) {
	if rt.scrollDrag.st != nil {
		rt.scrollDrag.st = nil
		rt.requestFrame()
		return
	}
	if button == 1 && rt.menuRelease() {
		return
	}
	s := rt.pressed
	if s == nil || button != rt.pressButton {
		return
	}
	rt.pressed = nil
	s.pressed = false
	if s.editor != nil {
		s.editor.release()
	}
	inside := Rect{s.vx, s.vy, s.vw, s.vh}.Contains(rt.pointerX, rt.pointerY)
	if inside && s.flags&flagDisabled == 0 {
		switch button {
		case 0:
			s.clicks++
		case 1:
			s.rightClicks++
		}
	}
	rt.setHover(rt.hitChain(rt.pointerX, rt.pointerY))
	rt.requestFrame()
}

func (rt *engine) scroll(dx, dy float32, mods Modifiers) {
	if mods&Shift != 0 && dx == 0 {
		dx, dy = dy, 0
	}
	for _, id := range rt.hitChain(rt.pointerX, rt.pointerY) {
		if s := rt.states[id]; s != nil && scrollBy(s, dx, dy) {
			rt.requestFrame()
			return
		}
	}
}

// dragTo returns the offset of content whose thumb moved by moved DIPs
// along a track with travel DIPs of room since the offset was from, the
// content scrolling as far as reach when the drag started and as far as
// now: the end of the track shows the end.
func dragTo(from float64, moved, travel float32, reach, now float64) float64 {
	off := from + float64(moved)*reach/float64(travel)
	if off >= reach {
		return max(now, 0)
	}
	return max(0, min(off, now))
}

// bars returns the scroll bars of the container being dragged, with the
// content's size as the drag started.
func (d *scrollDrag) bars(width float32) scrollGeometry {
	s := d.st
	return scrollBars(Rect{s.x, s.y, s.w, s.h}, float32(d.contentW), float32(d.contentH), float32(s.scrollX), float32(s.scrollY), s.flags, width)
}

// scrollBy scrolls a container by dx, dy within its content, and reports
// whether it moved.
func scrollBy(s *state, dx, dy float32) bool {
	x, y := s.scrollX, s.scrollY
	if dy != 0 && s.flags&flagScrollY != 0 {
		y = max(0, min(y+float64(dy), s.contentH-float64(s.h)))
	}
	if dx != 0 && s.flags&flagScrollX != 0 {
		x = max(0, min(x+float64(dx), s.contentW-float64(s.w)))
	}
	if x == s.scrollX && y == s.scrollY {
		return false
	}
	s.scrollTo(x, y)
	return true
}

// scrollKey scrolls with the keys that scroll pages in browsers the
// innermost container around the focus that can go that way, or under the
// pointer without a focus, and reports whether one moved.
func (rt *engine) scrollKey(mods Modifiers, key Key) bool {
	const line, far = 40, 1e9
	var dx, dy, page float32
	switch {
	case mods == 0 && key == KeyDown:
		dy = line
	case mods == 0 && key == KeyUp:
		dy = -line
	case mods == 0 && key == KeyRight:
		dx = line
	case mods == 0 && key == KeyLeft:
		dx = -line
	case mods == 0 && (key == KeyPageDown || key == KeySpace):
		page = 1
	case mods == 0 && key == KeyPageUp, mods == Shift && key == KeySpace:
		page = -1
	case (mods == 0 || mods == Cmd) && key == KeyHome, mods == Cmd && key == KeyUp && runtime.GOOS == "darwin":
		dy = -far
	case (mods == 0 || mods == Cmd) && key == KeyEnd, mods == Cmd && key == KeyDown && runtime.GOOS == "darwin":
		dy = far
	default:
		return false
	}
	scroller := func(id uint64) bool {
		s := rt.states[id]
		return s != nil && s.flags&(flagScrollX|flagScrollY) != 0
	}
	chain := rt.focusChain()
	if !slices.ContainsFunc(chain, scroller) && rt.pointerIn {
		chain = rt.hitChain(rt.pointerX, rt.pointerY)
	}
	for _, id := range chain {
		if !scroller(id) {
			continue
		}
		s := rt.states[id]
		if page != 0 {
			// A page keeps a line of the last one in view.
			dy = page * max(s.h-line, s.h/2)
		}
		if scrollBy(s, dx, dy) {
			rt.requestFrame()
			return true
		}
	}
	return false
}

// focusChain returns the focused element and its ancestors, innermost
// first.
func (rt *engine) focusChain() []uint64 {
	var chain []uint64
	for s := rt.states[rt.focused]; s != nil; s = rt.states[s.parent] {
		chain = append(chain, s.id)
		if s.parent == 0 {
			break
		}
	}
	return chain
}

// claimed reports whether an element around the focus, or the window,
// handles the key as a shortcut.
func (rt *engine) claimed(k keyEvent) bool { return rt.claimedBy(k, true) }

// claimedBy reports whether an element around the focus handles the key as
// a shortcut, or, when window is set, the window.
func (rt *engine) claimedBy(k keyEvent, window bool) bool {
	var chain []uint64
	for _, r := range rt.regs {
		if r.mods != k.mods || r.key != k.key {
			continue
		}
		if r.id == 0 {
			if window {
				return true
			}
			continue
		}
		if chain == nil {
			chain = rt.focusChain()
		}
		if slices.Contains(chain, r.id) {
			return true
		}
	}
	return false
}

func (rt *engine) keyDown(mods Modifiers, key Key) {
	k := keyEvent{mods, key}
	if (key == KeyContextMenu && mods == 0 || key == KeyF10 && mods == Shift) && !rt.claimed(k) && rt.menuKey() {
		return
	}
	if s := rt.states[rt.focused]; s != nil && s.editor != nil && s.flags&(flagEditable|flagSelectable) != 0 && s.editor.wants(k) {
		s.editor.queue = append(s.editor.queue, editEvent{kind: editKey, mods: mods, key: key})
		rt.blinkStart = time.Now()
		rt.requestFrame()
		return
	}
	if key == KeyTab && (mods == 0 || mods == Shift) && !rt.claimed(k) {
		rt.moveFocus(mods == Shift)
		rt.requestFrame()
		return
	}
	if (key == KeyEnter || key == KeySpace) && mods == 0 {
		// A focused button or link takes them before the window's
		// shortcuts, such as a dialog's default button; a toggle takes
		// Space before them, and Enter after.
		s := rt.states[rt.focused]
		window := s != nil && key == KeyEnter && s.flags&flagToggle != 0
		if s != nil && !rt.claimedBy(k, window) && s.flags&flagClickable != 0 && s.flags&flagDisabled == 0 {
			s.clicks++
			rt.focusVisible = true
			rt.requestFrame()
			return
		}
	}
	if !rt.claimed(k) && rt.scrollKey(mods, key) {
		return
	}
	rt.keys = append(rt.keys, k)
	rt.requestFrame()
}

// moveFocus focuses the next (or previous) element that takes the focus.
func (rt *engine) moveFocus(back bool) {
	n := len(rt.focusOrder)
	if n == 0 {
		return
	}
	i := -1
	for j, id := range rt.focusOrder {
		if id == rt.focused {
			i = j
			break
		}
	}
	switch {
	case i < 0 && back:
		i = n - 1
	case i < 0:
		i = 0
	case back:
		i = (i - 1 + n) % n
	default:
		i = (i + 1) % n
	}
	rt.focused = rt.focusOrder[i]
	rt.focusVisible = true
	rt.blinkStart = time.Now()
	if s := rt.states[rt.focused]; s != nil && s.editor != nil {
		s.editor.selectAll()
	}
	rt.reveal(rt.focused)
}

// routeKeys delivers the keys pressed since the last frame to the
// innermost element around the focus that handles them, else to the
// window's shortcuts.
func (rt *engine) routeKeys() {
	if len(rt.keys) == 0 {
		return
	}
	chain := rt.focusChain()
	for _, k := range rt.keys {
		target, found := uint64(0), false
		for _, id := range chain {
			for _, r := range rt.regs {
				if r.id == id && r.mods == k.mods && r.key == k.key {
					target, found = id, true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			for _, r := range rt.regs {
				if r.id == 0 && r.mods == k.mods && r.key == k.key {
					found = true
					break
				}
			}
		}
		if found {
			rt.delivered = append(rt.delivered, shortcutReg{target, k.mods, k.key})
		}
	}
	rt.keys = rt.keys[:0]
}

// shortcut registers that element id (0 for the window) handles mods+key
// and reports whether such a key was delivered to it.
func (rt *engine) shortcut(id uint64, mods Modifiers, key Key) bool {
	rt.nextRegs = append(rt.nextRegs, shortcutReg{id, mods, key})
	for i, d := range rt.delivered {
		if d.id == id && d.mods == mods && d.key == key {
			rt.delivered = append(rt.delivered[:i], rt.delivered[i+1:]...)
			rt.consumed = true
			return true
		}
	}
	return false
}

func (rt *engine) editEvent(ev editEvent) {
	s := rt.states[rt.focused]
	if s == nil || s.editor == nil {
		return
	}
	// Selectable text takes the menus' commands, as Copy, not text.
	if s.flags&flagEditable == 0 && (s.flags&flagSelectable == 0 || ev.kind != editCommand) {
		return
	}
	s.editor.queue = append(s.editor.queue, ev)
	rt.blinkStart = time.Now()
	rt.requestFrame()
}

// imeContext is how many runes around the selection input methods see.
const imeContext = 512

// updateTextInput tells the host where text input goes, and the text
// around the caret.
func (rt *engine) updateTextInput() {
	var t platform.TextInputState
	base := 0
	if s := rt.states[rt.focused]; s != nil && s.editor != nil && s.flags&flagEditable != 0 && rt.windowFocused {
		ed := s.editor
		r := ed.caretRect(s)
		t.Active = true
		t.Caret = platform.RectF{X: float64(r.X), Y: float64(r.Y), W: float64(r.W), H: float64(r.H)}
		if !ed.password {
			a, z := ed.selection()
			base = max(0, a-imeContext)
			end := min(len(ed.text), z+imeContext)
			t.Text, t.Start, t.End = string(ed.text[base:end]), a-base, z-base
		}
	}
	if t != rt.ime.state {
		rt.ime.state, rt.ime.base = t, base
		rt.host.setTextInput(t)
	}
}

// replaced makes an edit replace the runes an input method named, from
// the text it was last given, rather than the selection.
func (rt *engine) replaced(ev editEvent, sev platform.SurfaceEvent) editEvent {
	if sev.Replace && rt.ime.state.Active {
		n := len([]rune(rt.ime.state.Text))
		from, to := max(0, min(sev.From, n)), max(0, min(sev.To, n))
		ev.replace, ev.from, ev.to = true, rt.ime.base+min(from, to), rt.ime.base+max(from, to)
	}
	return ev
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// Clicked reports whether the element was clicked, with the primary
// button or by Enter or Space while focused, since the last frame.
func (e *Element) Clicked() bool {
	e.flags |= flagClickable
	if e.IsDisabled() || e.st.clicks == 0 {
		return false
	}
	e.c.rt.consumed = true
	return true
}

// Clicks returns how many times the element was clicked since the last
// frame.
func (e *Element) Clicks() int {
	e.flags |= flagClickable
	if e.IsDisabled() {
		return 0
	}
	if e.st.clicks > 0 {
		e.c.rt.consumed = true
	}
	return e.st.clicks
}

// DoubleClicked reports a double click on the element.
func (e *Element) DoubleClicked() bool {
	e.flags |= flagClickable
	if e.IsDisabled() || e.st.doubleClicks == 0 {
		return false
	}
	e.c.rt.consumed = true
	return true
}

// RightClicked reports a click with the secondary button, as for a
// context menu.
func (e *Element) RightClicked() bool {
	e.flags |= flagClickable
	if e.IsDisabled() || e.st.rightClicks == 0 {
		return false
	}
	e.c.rt.consumed = true
	return true
}

// Hovered reports whether the pointer is over the element.
func (e *Element) Hovered() bool {
	e.flags |= flagHover
	if e.IsDisabled() {
		return false
	}
	rt := e.c.rt
	if rt.pressed != nil && rt.pressed.id != e.id {
		return false
	}
	for _, id := range rt.hover {
		if id == e.id {
			return true
		}
	}
	return false
}

// Pressed reports whether the element is being pressed with the pointer.
func (e *Element) Pressed() bool {
	e.flags |= flagClickable | flagHover
	s := e.st
	return s.pressed && Rect{s.vx, s.vy, s.vw, s.vh}.Contains(e.c.rt.pointerX, e.c.rt.pointerY)
}

// Focused reports whether the element has the keyboard focus.
func (e *Element) Focused() bool { return e.c.rt.focused == e.id && e.c.rt.windowFocused }

// FocusVisible reports whether the element has the keyboard focus and
// should show it, because it came from the keyboard.
func (e *Element) FocusVisible() bool { return e.Focused() && e.c.rt.focusVisible }

// FocusWithin reports whether the element or one of its descendants has
// the keyboard focus.
func (e *Element) FocusWithin() bool {
	for _, id := range e.c.rt.focusChain() {
		if id == e.id {
			return true
		}
	}
	return false
}

// Focus gives the element the keyboard focus. Called in every frame, it
// keeps it there; AutoFocus gives it once.
func (e *Element) Focus() *Element {
	e.flags |= flagFocusable
	rt := e.c.rt
	if rt.focused != e.id {
		rt.focused = e.id
		rt.blinkStart = time.Now()
	}
	return e
}

// AutoFocus gives the element the keyboard focus in the frame it appears,
// as the first field of a dialog.
func (e *Element) AutoFocus() *Element {
	if e.st.born == e.c.rt.frame {
		e.Focus()
	}
	return e
}

// Shortcut reports whether the key with exactly the modifiers mods was
// pressed while the element or one of its descendants had the focus. The
// innermost element handling a key gets it.
func (e *Element) Shortcut(mods Modifiers, key Key) bool {
	return e.c.rt.shortcut(e.id, mods, key)
}

// PointerPosition returns the pointer's position relative to the
// element's box and whether it is over the element. Elements asking for it
// get a frame whenever the pointer moves over them.
func (e *Element) PointerPosition() (x, y float32, over bool) {
	e.flags |= flagTrackPointer
	rt := e.c.rt
	s := e.st
	x, y = rt.pointerX-s.x, rt.pointerY-s.y
	return x, y, rt.pointerIn && Rect{s.vx, s.vy, s.vw, s.vh}.Contains(rt.pointerX, rt.pointerY)
}

// Dragged reports how far the pointer moved since the last frame while
// pressing the element.
func (e *Element) Dragged() (dx, dy float32, ok bool) {
	e.flags |= flagDraggable
	s := e.st
	if !s.pressed {
		return 0, 0, false
	}
	if s.dragX != 0 || s.dragY != 0 {
		e.c.rt.consumed = true
	}
	return s.dragX, s.dragY, true
}

// Changed reports whether a widget's value changed since the last frame.
func (e *Element) Changed() bool { return e.st.changed }

// Submitted reports whether Enter was pressed in a single-line text input.
func (e *Element) Submitted() bool { return e.st.submitted }

// scrollbarPress starts dragging the thumb of the scroll container under
// the pointer when the press is on its scroll bar, or pages toward the
// press on the bar's track.
func (rt *engine) scrollbarPress(chain []uint64, x, y float32) bool {
	for _, id := range chain {
		s := rt.states[id]
		if s == nil || s.flags&(flagScrollX|flagScrollY) == 0 {
			continue
		}
		g := scrollBars(Rect{s.x, s.y, s.w, s.h}, float32(s.contentW), float32(s.contentH), float32(s.scrollX), float32(s.scrollY), s.flags, rt.c.theme.scrollbarWidth())
		d := &rt.scrollDrag
		w, h := float64(s.w), float64(s.h)
		switch {
		case g.vertical && g.vTrack.Contains(x, y):
			switch {
			case y >= g.v.Y && y < g.v.Y+g.v.H:
				d.st, d.start, d.from, d.horizontal = s, y, s.scrollY, false
				d.contentW, d.contentH = s.contentW, s.contentH
			case y < g.v.Y:
				s.scrollTo(s.scrollX, max(0, s.scrollY-h*0.9))
			default:
				s.scrollTo(s.scrollX, min(s.contentH-h, s.scrollY+h*0.9))
			}
		case g.horizontal && g.hTrack.Contains(x, y):
			switch {
			case x >= g.h.X && x < g.h.X+g.h.W:
				d.st, d.start, d.from, d.horizontal = s, x, s.scrollX, true
				d.contentW, d.contentH = s.contentW, s.contentH
			case x < g.h.X:
				s.scrollTo(max(0, s.scrollX-w*0.9), s.scrollY)
			default:
				s.scrollTo(min(s.contentW-w, s.scrollX+w*0.9), s.scrollY)
			}
		default:
			continue
		}
		rt.requestFrame()
		return true
	}
	return false
}
