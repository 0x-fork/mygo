package ui

import (
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/text"
)

type editKind uint8

const (
	editKey editKind = iota
	editInsert
	editCompose
	editCommand
)

type editEvent struct {
	kind  editKind
	mods  Modifiers
	key   Key
	text  string
	caret int
	// replace makes an insertion or a composition take the runes from to
	// to instead of the selection, as an input method asked.
	replace  bool
	from, to int
}

type snapshot struct {
	text          string
	caret, anchor int
}

// editor is the state of a text input: the caret, the selection, the
// composition of an input method, undo history and what the last frame
// laid out.
type editor struct {
	text          []rune
	caret, anchor int
	compose       string
	composeCaret  int
	queue         []editEvent
	multiline     bool
	readOnly      bool   // selectable text: selected and copied, not edited
	source        string // the text of selectable text
	password      bool
	// leaveEmptyBackspace leaves Backspace to shortcuts while the text is
	// empty, as a token field's input does to take out a token.
	leaveEmptyBackspace bool
	placeholder         string
	bounds              text.Boundaries
	boundsValid         bool
	layout              *text.Layout
	// display maps runes of the text to runes of the layout, which shows
	// bullets for passwords and holds the composition.
	scrollX, scrollY float32
	originX, originY float32 // content box, relative to the element
	desiredX         float32
	hasDesired       bool
	undo, redo       []snapshot
	lastEdit         time.Time
	coalesce         bool
	dragging         bool
	dragUnit         int // 1 rune, 2 word, 3 line
	dragStart        [2]int
	pressMods        Modifiers
	contentW         float32
}

func newEditor() *editor { return &editor{} }

func (ed *editor) setText(s string) {
	ed.text = []rune(s)
	ed.boundsValid = false
	ed.caret = min(ed.caret, len(ed.text))
	ed.anchor = min(ed.anchor, len(ed.text))
}

func (ed *editor) String() string { return string(ed.text) }

func (ed *editor) boundaries() *text.Boundaries {
	if !ed.boundsValid {
		ed.bounds.Reset(ed.text)
		ed.boundsValid = true
	}
	return &ed.bounds
}

func (ed *editor) selection() (int, int) {
	if ed.caret < ed.anchor {
		return ed.caret, ed.anchor
	}
	return ed.anchor, ed.caret
}

func (ed *editor) selectAll() { ed.anchor, ed.caret = 0, len(ed.text) }

// wants reports whether the editor handles a key, rather than letting it
// reach shortcuts.
func (ed *editor) wants(k keyEvent) bool {
	m := k.mods &^ Shift
	if ed.readOnly {
		// Selecting and copying; the arrows without Shift scroll.
		switch k.key {
		case KeyA, KeyC:
			return k.mods == Cmd
		case KeyLeft, KeyRight, KeyUp, KeyDown, KeyHome, KeyEnd:
			return k.mods&Shift != 0
		}
		return false
	}
	switch k.key {
	case KeyBackspace:
		// Empty, a token field's input leaves it to take out a token.
		return !ed.leaveEmptyBackspace || len(ed.text) > 0 || m != 0
	case KeyLeft, KeyRight, KeyHome, KeyEnd, KeyDelete:
		return true
	case KeyUp, KeyDown, KeyPageUp, KeyPageDown:
		return ed.multiline || m != 0
	case KeyEnter:
		return m == 0
	case KeyTab, KeyEscape:
		return false
	case KeyA, KeyC, KeyX, KeyV, KeyZ, KeyY:
		if m == Cmd {
			return true
		}
	}
	if m == Ctrl && runtime.GOOS == "darwin" {
		if _, ok := emacsKeys[k.key]; ok || k.key == KeyA || k.key == KeyE || k.key == KeyK {
			return true
		}
	}
	// Printable keys type text, which comes as TextInput.
	return m == 0 || m == Alt && runtime.GOOS == "darwin"
}

func (ed *editor) snapshot() snapshot {
	return snapshot{string(ed.text), ed.caret, ed.anchor}
}

func (ed *editor) restore(s snapshot) {
	ed.setText(s.text)
	ed.caret, ed.anchor = s.caret, s.anchor
}

// record saves the state before an edit for undo; typing in a row makes
// one step.
func (ed *editor) record(typing bool) {
	now := time.Now()
	if typing && ed.coalesce && now.Sub(ed.lastEdit) < time.Second {
		ed.lastEdit = now
		return
	}
	ed.undo = append(ed.undo, ed.snapshot())
	if len(ed.undo) > 200 {
		ed.undo = ed.undo[1:]
	}
	ed.redo = ed.redo[:0]
	ed.coalesce = typing
	ed.lastEdit = now
}

func (ed *editor) replace(start, end int, s string) {
	ins := []rune(s)
	if !ed.multiline {
		ins = []rune(strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' {
				return ' '
			}
			return r
		}, s))
	}
	out := make([]rune, 0, len(ed.text)-(end-start)+len(ins))
	out = append(out, ed.text[:start]...)
	out = append(out, ins...)
	out = append(out, ed.text[end:]...)
	ed.text = out
	ed.boundsValid = false
	ed.caret = start + len(ins)
	ed.anchor = ed.caret
}

func (ed *editor) insert(s string) {
	if ed.readOnly {
		return
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	a, b := ed.selection()
	ed.record(a == b && utf8.RuneCountInString(s) == 1)
	ed.replace(a, b, s)
	ed.hasDesired = false
}

func (ed *editor) deleteRange(a, b int) {
	if a == b || ed.readOnly {
		return
	}
	ed.record(false)
	ed.replace(a, b, "")
	ed.hasDesired = false
}

func (ed *editor) move(to int, extend bool) {
	to = max(0, min(to, len(ed.text)))
	ed.caret = to
	if !extend {
		ed.anchor = to
	}
	ed.coalesce = false
}

// lineStart and lineEnd return the edges of the visual line holding the
// caret.
func (ed *editor) lineEdges(i int) (int, int) {
	if ed.layout == nil || len(ed.layout.Lines) == 0 {
		return 0, len(ed.text)
	}
	li := ed.layout.LineAt(ed.displayIndex(i))
	line := ed.layout.Lines[li]
	return ed.textIndex(line.Start), ed.textIndex(line.End)
}

// displayIndex maps a rune of the text to the layout, which holds the
// composition at the caret.
func (ed *editor) displayIndex(i int) int {
	if ed.compose != "" && i > ed.caret {
		return i + utf8.RuneCountInString(ed.compose)
	}
	return i
}

func (ed *editor) textIndex(i int) int {
	if ed.compose != "" {
		n := utf8.RuneCountInString(ed.compose)
		switch {
		case i > ed.caret+n:
			return i - n
		case i > ed.caret:
			return ed.caret
		}
	}
	return min(i, len(ed.text))
}

// vertical moves the caret up or down lines, keeping its x.
func (ed *editor) vertical(lines int, extend bool) {
	if ed.layout == nil {
		return
	}
	x, y, h := ed.layout.Caret(ed.displayIndex(ed.caret))
	if !ed.hasDesired {
		ed.desiredX, ed.hasDesired = x, true
	}
	target := y + h/2 + float32(lines)*h
	if target < 0 {
		ed.move(0, extend)
		return
	}
	if target > ed.layout.Height {
		ed.move(len(ed.text), extend)
		return
	}
	ed.move(ed.textIndex(ed.layout.IndexAt(ed.desiredX, target)), extend)
}

// emacsKeys are the Control keys of macOS text fields that act as other
// keys, after Emacs.
var emacsKeys = map[Key]Key{KeyB: KeyLeft, KeyF: KeyRight, KeyP: KeyUp, KeyN: KeyDown, KeyH: KeyBackspace, KeyD: KeyDelete}

func (ed *editor) key(c *Context, st *state, k editEvent) {
	shift := k.mods&Shift != 0
	m := k.mods &^ Shift
	mac := runtime.GOOS == "darwin"
	if mac && m == Ctrl {
		if to, ok := emacsKeys[k.key]; ok {
			k.key, m = to, 0
		} else if ed.emacsKey(k.key, shift) {
			return
		}
	}
	word := (!mac && m == Ctrl) || (mac && m == Alt)
	b := ed.boundaries()
	a, z := ed.selection()
	switch k.key {
	case KeyLeft, KeyRight:
		left := k.key == KeyLeft
		switch {
		case mac && m == Super:
			s, e := ed.lineEdges(ed.caret)
			if left {
				ed.move(s, shift)
			} else {
				ed.move(e, shift)
			}
		case word && left:
			ed.move(b.PrevWord(ed.caret), shift)
		case word:
			ed.move(b.NextWord(ed.caret), shift)
		case a != z && !shift && left:
			ed.move(a, false)
		case a != z && !shift:
			ed.move(z, false)
		case left:
			ed.move(b.PrevGrapheme(ed.caret), shift)
		default:
			ed.move(b.NextGrapheme(ed.caret), shift)
		}
		ed.hasDesired = false
		return
	case KeyUp, KeyDown:
		if mac && m == Super || !ed.multiline {
			if k.key == KeyUp {
				ed.move(0, shift)
			} else {
				ed.move(len(ed.text), shift)
			}
			return
		}
		d := 1
		if k.key == KeyUp {
			d = -1
		}
		ed.vertical(d, shift)
		return
	case KeyPageUp, KeyPageDown:
		n := max(int(st.h/max(ed.lineHeight(), 1))-1, 1)
		if k.key == KeyPageUp {
			n = -n
		}
		ed.vertical(n, shift)
		return
	case KeyHome, KeyEnd:
		if m == Ctrl || !ed.multiline {
			if k.key == KeyHome {
				ed.move(0, shift)
			} else {
				ed.move(len(ed.text), shift)
			}
			return
		}
		s, e := ed.lineEdges(ed.caret)
		if k.key == KeyHome {
			ed.move(s, shift)
		} else {
			ed.move(e, shift)
		}
		return
	case KeyBackspace:
		switch {
		case a != z:
			ed.deleteRange(a, z)
		case mac && m == Super:
			s, _ := ed.lineEdges(ed.caret)
			ed.deleteRange(s, ed.caret)
		case word:
			ed.deleteRange(b.PrevWord(ed.caret), ed.caret)
		case ed.caret > 0:
			ed.record(true)
			ed.replace(b.PrevGrapheme(ed.caret), ed.caret, "")
		}
		ed.hasDesired = false
		return
	case KeyDelete:
		switch {
		case a != z:
			ed.deleteRange(a, z)
		case word:
			ed.deleteRange(ed.caret, b.NextWord(ed.caret))
		case ed.caret < len(ed.text):
			ed.deleteRange(ed.caret, b.NextGrapheme(ed.caret))
		}
		return
	case KeyEnter:
		if ed.multiline {
			ed.insert("\n")
		} else {
			st.submitted = true
			c.rt.consumed = true
		}
		return
	}
	if m != Cmd {
		return
	}
	switch k.key {
	case KeyA:
		ed.selectAll()
	case KeyC:
		ed.command(c, "copy")
	case KeyX:
		ed.command(c, "cut")
	case KeyV:
		ed.command(c, "paste")
	case KeyZ:
		if shift {
			ed.command(c, "redo")
		} else {
			ed.command(c, "undo")
		}
	case KeyY:
		ed.command(c, "redo")
	}
}

// emacsKey performs Control-A, -E and -K of macOS text fields, which move
// to the start and end of the paragraph and delete to its end, and reports
// whether key was one of them.
func (ed *editor) emacsKey(key Key, shift bool) bool {
	start, end := ed.caret, ed.caret
	for start > 0 && ed.text[start-1] != '\n' {
		start--
	}
	for end < len(ed.text) && ed.text[end] != '\n' {
		end++
	}
	switch key {
	case KeyA:
		ed.move(start, shift)
	case KeyE:
		ed.move(end, shift)
	case KeyK:
		if end == ed.caret && end < len(ed.text) {
			end++ // at the end, join the next paragraph
		}
		ed.anchor = ed.caret
		ed.deleteRange(ed.caret, end)
	default:
		return false
	}
	ed.hasDesired = false
	return true
}

func (ed *editor) command(c *Context, name string) {
	a, b := ed.selection()
	h := c.rt.host
	if ed.readOnly && name != "copy" && name != "selectAll" {
		return
	}
	switch name {
	case "copy":
		if a != b && !ed.password {
			h.writeClipboard(string(ed.text[a:b]))
		}
	case "cut":
		if a != b && !ed.password {
			h.writeClipboard(string(ed.text[a:b]))
			ed.deleteRange(a, b)
		}
	case "paste":
		if s := h.readClipboard(); s != "" {
			ed.insert(s)
		}
	case "selectAll":
		ed.selectAll()
	case "delete":
		ed.deleteRange(a, b)
	case "undo":
		if n := len(ed.undo); n > 0 {
			ed.redo = append(ed.redo, ed.snapshot())
			ed.restore(ed.undo[n-1])
			ed.undo = ed.undo[:n-1]
			ed.coalesce = false
		}
	case "redo":
		if n := len(ed.redo); n > 0 {
			ed.undo = append(ed.undo, ed.snapshot())
			ed.restore(ed.redo[n-1])
			ed.redo = ed.redo[:n-1]
			ed.coalesce = false
		}
	}
}

// press puts the caret where the pointer went down, at (x, y) relative to
// the element; double and triple clicks select words and lines.
func (ed *editor) press(x, y float32, clicks, button int) {
	if button != 0 || ed.layout == nil {
		return
	}
	ed.commitCompose()
	i := ed.hit(x, y)
	ed.dragging = true
	ed.hasDesired = false
	ed.dragUnit = min(clicks, 3)
	switch ed.dragUnit {
	case 1:
		ed.move(i, ed.pressMods&Shift != 0)
	case 2:
		s, e := ed.boundaries().WordAt(i)
		ed.anchor, ed.caret = s, e
	default:
		if ed.multiline {
			s, e := ed.lineEdges(i)
			ed.anchor, ed.caret = s, e
		} else {
			ed.selectAll()
		}
	}
	ed.dragStart = [2]int{ed.anchor, ed.caret}
}

func (ed *editor) release() { ed.dragging = false }

func (ed *editor) hit(x, y float32) int {
	if ed.layout == nil {
		return 0
	}
	return ed.textIndex(ed.layout.IndexAt(x-ed.originX+ed.scrollX, y-ed.originY+ed.scrollY))
}

// drag extends the selection to the pointer.
func (ed *editor) drag(x, y float32) {
	i := ed.hit(x, y)
	switch ed.dragUnit {
	case 2:
		s, e := ed.boundaries().WordAt(i)
		if i < ed.dragStart[0] {
			ed.anchor, ed.caret = ed.dragStart[1], s
		} else {
			ed.anchor, ed.caret = ed.dragStart[0], max(e, ed.dragStart[1])
		}
	case 3:
	default:
		ed.caret = i
	}
}

func (ed *editor) commitCompose() {
	if ed.compose != "" {
		ed.compose = ""
	}
}

func (ed *editor) lineHeight() float32 {
	if ed.layout != nil && len(ed.layout.Lines) > 0 {
		return ed.layout.Lines[0].Height
	}
	return 18
}

// caretRect returns the caret's box relative to the surface, where input
// methods show their candidates.
func (ed *editor) caretRect(st *state) Rect {
	if ed.layout == nil {
		return Rect{st.x, st.y, 1, st.h}
	}
	x, y, h := ed.layout.Caret(ed.displayIndex(ed.caret) + ed.composeCaret)
	return Rect{st.x + ed.originX + x - ed.scrollX, st.y + ed.originY + y - ed.scrollY, 1, h}
}

// process applies the input queued for the editor.
func (ed *editor) process(c *Context, e *Element) {
	st := e.st
	for _, ev := range ed.queue {
		if ev.replace {
			ed.anchor, ed.caret = min(ev.from, len(ed.text)), min(ev.to, len(ed.text))
		}
		switch ev.kind {
		case editKey:
			ed.commitCompose()
			ed.key(c, st, ev)
		case editInsert:
			ed.compose = ""
			ed.insert(ev.text)
		case editCompose:
			ed.compose = ev.text
			ed.composeCaret = max(0, min(ev.caret, utf8.RuneCountInString(ev.text)))
			if ev.text != "" {
				a, b := ed.selection()
				if a != b {
					ed.deleteRange(a, b)
				}
			}
		case editCommand:
			ed.commitCompose()
			ed.command(c, ev.text)
		}
	}
	ed.queue = ed.queue[:0]
	if ed.dragging && st.pressed {
		rt := c.rt
		ed.drag(rt.pointerX-st.x, rt.pointerY-st.y)
	}
}

// TextInput creates a single-line text input editing *value.
func TextInput(c *Context, value *string) *Element { return textInput(c, value, false) }

// TextArea creates a multi-line text input editing *value.
func TextArea(c *Context, value *string) *Element { return textInput(c, value, true) }

func textInput(c *Context, value *string, multiline bool) *Element {
	t := c.theme
	e := textInputBase(c, value, multiline)
	e.Padding(t.Space(1.5), t.Space(2.5)).Radius(t.Radius).Background(t.Surface).Border(1, t.Border)
	if multiline {
		e.MinHeight(t.Space(20))
	}
	e.styleFn = func(e *Element) { inputBorder(t, e, e) }
	return e
}

func textInputBase(c *Context, value *string, multiline bool) *Element {
	e := c.newElement(kindInput)
	e.flags |= flagEditable | flagFocusable | flagHover
	e.widget = "TextInput"
	if multiline {
		e.widget = "TextArea"
	}
	st := e.st
	if st.editor == nil {
		st.editor = newEditor()
		st.editor.setText(*value)
		st.editor.caret, st.editor.anchor = len(st.editor.text), len(st.editor.text)
	}
	ed := st.editor
	ed.multiline = multiline
	if string(ed.text) != *value {
		ed.setText(*value)
		ed.compose = ""
	}
	focused := c.rt.focused == e.id
	// Input queued while the input had the focus applies even when the
	// focus left before this frame, as with text typed right before Tab.
	if focused || len(ed.queue) > 0 {
		before := string(ed.text)
		ed.process(c, e)
		if after := string(ed.text); after != before {
			*value = after
			st.changed = true
			c.rt.consumed = true
		}
	}
	if !focused {
		ed.compose = ""
	}
	return e
}

// Placeholder shows s in an empty text input.
func (e *Element) Placeholder(s string) *Element {
	if e.st.editor != nil {
		e.st.editor.placeholder = s
	}
	return e
}

// Password hides what a text input holds.
func (e *Element) Password() *Element {
	if e.st.editor != nil {
		e.st.editor.password = true
	}
	return e
}

// Selectable lets the user select the text of a Text element, by dragging
// over it, double-clicking a word or triple-clicking a line, and copy it:
// a click gives it the keyboard focus, for Shift with the arrows and
// Cmd+C, and its context menu has Copy and Select All.
func (e *Element) Selectable() *Element {
	if e.kind != kindText {
		return e
	}
	e.flags |= flagSelectable
	st := e.st
	ed := st.editor
	if ed == nil {
		ed = newEditor()
		ed.readOnly, ed.multiline = true, true
		st.editor = ed
	}
	if ed.source != e.text {
		ed.source = e.text
		ed.setText(e.text)
		ed.caret, ed.anchor = 0, 0
	}
	if e.c.rt.focused == e.id || len(ed.queue) > 0 {
		ed.process(e.c, e)
	}
	return e
}

// displayText returns what the input shows: the text with the
// composition at the caret, bullets for a password.
func (ed *editor) displayText() string {
	t := ed.text
	if ed.compose != "" {
		c := []rune(ed.compose)
		d := make([]rune, 0, len(t)+len(c))
		d = append(d, t[:ed.caret]...)
		d = append(d, c...)
		d = append(d, t[ed.caret:]...)
		t = d
	}
	if ed.password {
		return strings.Repeat("•", len(t))
	}
	return string(t)
}

func (e *Element) inputParams(width float32) text.Params {
	p := e.textParams(width)
	p.Text = e.st.editor.displayText()
	p.KeepSpaces = true
	p.MaxLines = 0
	if !e.st.editor.multiline {
		p.Width = 0
	}
	return p
}

func (e *Element) inputHeight() float32 {
	ed := e.st.editor
	l := textSystem().Layout(e.inputParams(0))
	if ed.multiline {
		return max(l.Height, 3*l.Lines[0].Height)
	}
	return l.Lines[0].Height
}

func (e *Element) layoutInput(cw, ch float32) {
	ed := e.st.editor
	l := textSystem().Layout(e.inputParams(cw))
	ed.layout = l
	ed.contentW = cw
	ed.originX, ed.originY = e.contentX(), e.contentY()
	if ed.multiline && len(l.Lines) > 0 && l.Height < ch {
		// Center a single line vertically in a taller box.
		_ = ch
	}
	if !ed.multiline && len(l.Lines) > 0 {
		ed.originY += max((ch-l.Lines[0].Height)/2, 0)
	}
	// Keep the caret in view.
	x, y, h := l.Caret(ed.displayIndex(ed.caret) + ed.composeCaret)
	if ed.multiline {
		ed.scrollX = 0
		if y-ed.scrollY < 0 {
			ed.scrollY = y
		} else if y+h-ed.scrollY > ch {
			ed.scrollY = y + h - ch
		}
		ed.scrollY = max(0, min(ed.scrollY, max(l.Height-ch, 0)))
	} else {
		ed.scrollY = 0
		if x-ed.scrollX < 0 {
			ed.scrollX = x
		} else if x-ed.scrollX > cw-1 {
			ed.scrollX = x - cw + 1
		}
		ed.scrollX = max(0, min(ed.scrollX, max(l.Width-cw+1, 0)))
	}
}

func (e *Element) paintInput(p *Painter) {
	ed := e.st.editor
	l := ed.layout
	t := e.c.theme
	if l == nil {
		return
	}
	box := e.contentBox()
	clip := Rect{e.x + e.border[3], e.y + e.border[0], e.w - e.border[1] - e.border[3], e.h - e.border[0] - e.border[2]}
	saved := p.clip
	p.pushClip(clip, [4]float32{})
	ox, oy := e.x+ed.originX-ed.scrollX, e.y+ed.originY-ed.scrollY
	focused := e.Focused()
	ts := e.resolvedText()
	if len(ed.text) == 0 && ed.compose == "" && ed.placeholder != "" {
		pl := textSystem().Layout(text.Params{Text: ed.placeholder, Style: text.Style{Family: ts.family, Size: ts.size, Weight: ts.weight, LineHeight: ts.lineHeight}, Width: box.W})
		p.textLayout(pl, ox, oy, t.TextMuted, ts, nil)
	}
	if a, b := ed.selection(); a != b && focused {
		for _, r := range l.Selection(ed.displayIndex(a), ed.displayIndex(b)) {
			p.Fill(Rect{ox + r.X, oy + r.Y, r.W, r.H}, t.Selection, 0)
		}
	}
	p.textLayout(l, ox, oy, ts.color, ts, nil)
	if ed.compose != "" {
		start := ed.caret
		end := start + utf8.RuneCountInString(ed.compose)
		for _, r := range l.Selection(start, end) {
			p.Fill(Rect{ox + r.X, oy + r.Y + r.H - 2, r.W, 1}, ts.color, 0)
		}
	}
	if focused {
		rt := e.c.rt
		phase := time.Since(rt.blinkStart)
		const blink = 530 * time.Millisecond
		if (phase/blink)%2 == 0 {
			x, y, h := l.Caret(ed.displayIndex(ed.caret) + ed.composeCaret)
			p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpFill, Rect: p.snap(Rect{ox + x, oy + y, 0, h}), Color: t.Accent.scene(), Opacity: p.opacity})
			op := &p.s.Ops[len(p.s.Ops)-1]
			op.Rect.W = max(round(p.scale), 1)
		}
		if phase < 30*time.Second {
			e.c.After(blink - phase%blink)
		}
	}
	p.popClip()
	p.clip = saved
}
