package ui

import (
	"math/bits"
	"slices"
)

// A List builds only the rows in view, and keeps its place by a row, its
// anchor, and how far its top is from the top of the list, rather than by
// an offset into its content: rows above and below may change height as
// they are measured, rows may come and go, and the rows in view stay
// where they are. Building, it makes the rows its place shows, as far as
// the heights it knows tell; laying out, it measures them, places them
// from the anchor, and builds the rows still missing, so a frame never
// shows a gap. The offset of its content, which its scroll bar shows and
// the wheel moves, is where the heights known (measured, or estimated as
// their average) put the anchor, in float64: millions of rows scroll by
// fractions of a DIP.

// listOverscan is how many rows a List builds beyond each edge of its
// view, for Tab to move the focus to and assistive technology to read.
const listOverscan = 2

// listMaxRows bounds the rows a List builds in a frame, against rows
// taking no room.
const listMaxRows = 4096

// listSearch is how far from where it was a List looks for an item's key
// when its rows changed.
const listSearch = 1000

// ListState is a List's place among its rows, how it behaves, and what it
// knows of the heights of its rows, kept in the app's state. Give each
// List a ListState of its own; set its fields before building the List.
type ListState struct {
	// Key returns an identity for the item row i shows, such as its ID:
	// comparable, and unique among the rows. Without it, a row is its
	// index. With it, the state of a row (focus, text being edited, …)
	// follows its item, and the list keeps its place and its choice when
	// rows are added or removed above them, as when older messages load.
	Key func(row int) any
	// FollowEnd starts the list at its end, and keeps the end in view as
	// rows are added or grow while it shows it, as a chat or a log does:
	// scrolling away from the end stops following it, and scrolling back
	// to it follows again.
	FollowEnd bool
	// Selected, when set, lets a click choose a row, as Up, Down, Home and
	// End do while the list has the keyboard focus: *Selected is the
	// chosen row, -1 for none, which shows in the accent color. The list's
	// Changed reports a new choice, and its Submitted a double click or
	// Enter.
	Selected *int
	// Header, when set, reports whether row i heads a section: the header
	// of the section at the top of the list stays there while the rows of
	// its section scroll under it, until the next header pushes it away.
	// Give headers a background. Headers are not chosen.
	Header func(row int) bool

	// The place: the first row in view, and how far the list is scrolled
	// past its top: the row's top is that far above where the content
	// starts, so that the offset is the row's plus inset.
	anchor    int
	inset     float64
	anchorKey any
	following bool
	started   bool
	laidOut   bool
	// wroteY is the offset the list set, to tell it from scrolling.
	wroteY  float64
	req     listRequest
	heights listHeights
	// width, gap and padTop are those the rows were placed with: the width
	// of the rows, the gap between them, and where the first starts below
	// the list's top. border is its top border.
	width       float32
	gap         float64
	padTop      float64
	border      float64
	first, last int
	atEnd       bool
	// read is set when the view read where the list is, which a frame
	// that moves it then builds another for.
	read bool
	// selRow is the row chosen in the last frame and selKey its key, to
	// tell the app choosing another from rows moving.
	selRow int
	selKey any
	header listHeader
	// rows are the last frame's rows by their elements' IDs, to find the
	// row holding the focus.
	rows  map[uint64]listRowID
	frame listFrame
}

// listRequest is where the app or the user asked the list to go: to row
// at align, as little as shows it (near), to its end, or to offset y
// (with row, when hint is set, the row to place from there).
type listRequest struct {
	set, end, near, offset, hint bool
	row                          int
	align                        Align
	y                            float64
}

type listRowID struct {
	row int
	key any
}

// ScrollTo scrolls the list to show row at align: Start shows it at the
// top, Center in the middle and End at the bottom. Call it from the view:
// the list scrolls as the frame is laid out, to where the row is once it
// is measured.
func (s *ListState) ScrollTo(row int, align Align) {
	s.req = listRequest{set: true, row: row, align: align}
}

// ScrollIntoView scrolls the list as little as shows row whole, as
// Element.ScrollIntoView does an element.
func (s *ListState) ScrollIntoView(row int) {
	s.req = listRequest{set: true, row: row, near: true}
}

// ScrollToEnd scrolls the list to its end, where FollowEnd follows it
// again.
func (s *ListState) ScrollToEnd() { s.req = listRequest{set: true, end: true} }

// Visible returns the first and the last row the list showed in the last
// frame, last below first when it showed none: a list nearing the end of
// what was loaded, say, loads more. A frame that changes them builds
// another, for what read them.
func (s *ListState) Visible() (first, last int) {
	s.read = true
	if !s.laidOut {
		return 0, -1
	}
	return s.first, s.last
}

// AtEnd reports whether the list showed its end in the last frame, as
// Visible does its rows.
func (s *ListState) AtEnd() bool {
	s.read = true
	return s.atEnd
}

// listFrame is what a List built in the frame, for laying it out.
type listFrame struct {
	c *Context
	e *Element
	// owner takes the focus and reports choices: the list, or its Table.
	owner *Element
	s     *ListState
	n     int
	row   func(i int)
	frame uint64
	pass  int
	// flat leaves the corners of chosen rows square, as a Table's.
	flat bool
	// theme is the theme the rows are built with, laying out as well.
	theme *Theme
	rows  []listRow
	// at finds rows by their index while the list lays out.
	at map[int]int
}

type listRow struct {
	i   int
	key any
	e   *Element
	y   float64
}

// List creates a vertical scroll container of n rows, built with row(i),
// which builds only those in view, and a few beyond. Rows take the height
// of their content: the list measures them as they show and estimates the
// others from them, so a list of millions of rows is as fast as one of
// ten. Give it a size, or Grow it in its parent:
//
//	ui.List(c, nil, len(app.files), func(i int) {
//		ui.Text(c, app.files[i].Name).Padding(6, 12)
//	}).Grow(1)
//
// s keeps its place and says how it behaves, or nil keeps its place in
// the list's own state: with a ListState, the app scrolls the list to a
// row, rows keep their state with their items, the list follows its end,
// chooses rows, and pins the headers of sections. Gap spaces the rows,
// Padding pads the content, scrolling with it, and Justify(End) puts rows
// that do not fill the list at its bottom, as a chat's first messages.
func List(c *Context, s *ListState, n int, row func(i int)) *Element {
	e := Scroll(c)
	e.widget, e.role = "List", RoleList
	if s == nil {
		s = Local(e, "list", func() ListState { return ListState{} })
	}
	buildList(c, e, e, s, n, row, false)
	return e
}

// buildList builds the rows of list e that its place shows, with owner
// taking the focus and reporting choices.
func buildList(c *Context, e, owner *Element, s *ListState, n int, row func(i int), flat bool) {
	rt := c.rt
	f := &s.frame
	if f.e != nil && f.frame == rt.frame && f.pass == rt.pass {
		panic("ui: a ListState shown by two Lists")
	}
	n = max(n, 0)
	*f = listFrame{c: c, e: e, owner: owner, s: s, n: n, row: row, frame: rt.frame, pass: rt.pass, flat: flat, theme: c.theme, rows: f.rows[:0], at: f.at}
	e.list, owner.rowsOf = f, f
	s.sync(e, n)
	if s.Selected != nil {
		if owner == e {
			e.Focusable()
		}
		f.navigate()
	}
	a, first, last := s.plan(e, n)
	e.Children(func() {
		for i := first; i < last; i++ {
			f.build(i)
		}
		// The row holding the focus stays out of view as well, with the
		// text being edited in it; and the header of the section at the
		// top is built where the others are.
		focus, ok := s.focusedRow(e, n)
		if ok && (focus < first || focus >= last) {
			f.build(focus)
		}
		if s.Header != nil && n > 0 {
			if h := s.headerAbove(a, n); h >= 0 && (h < first || h >= last) && (!ok || h != focus) {
				f.build(h)
			}
		}
	})
}

// sync follows what changed since the last frame: rows added or removed,
// items moving, the user or the app scrolling.
func (s *ListState) sync(e *Element, n int) {
	st, rt := e.st, e.c.rt
	if !s.started {
		s.started = true
		s.following = s.FollowEnd
		s.selRow = -1
		s.header.row = -1
	}
	s.heights.def = float64(e.c.theme.Space(8))
	if s.Key != nil && n > 0 {
		if k := s.anchorKey; k != nil {
			if j, ok := s.find(k, s.anchor, n); ok && j != s.anchor {
				// Rows came or went above it: the heights known move with
				// the rows around it.
				s.heights.resize(max(n, s.heights.n))
				s.heights.shift(j - s.anchor)
				s.anchor = j
				s.header.ok = false
			}
		}
		if sel := s.Selected; sel != nil && *sel == s.selRow && s.selKey != nil {
			// The choice follows its item, and is gone with it.
			j, ok := s.find(s.selKey, *sel, n)
			if !ok {
				j = -1
			}
			*sel, s.selRow = j, j
		}
	}
	if n != s.heights.n {
		s.heights.resize(n)
		s.header.ok = false
	}
	s.anchor = max(0, min(s.anchor, n-1))
	switch {
	case st.list != s || st.born == rt.frame:
		// Built anew, as when its page shows again, or showing another
		// place: the place stands, unless the app set the offset with a
		// ScrollState this frame.
		if st.movedIn(rt.frame) && !s.req.set {
			s.req = listRequest{set: true, offset: true, y: max(0, st.scrollY)}
		}
		st.list = s
	case st.scrollY != s.wroteY && !s.req.set:
		// Scrolled by the wheel, the keys, the scroll bar or the app: a
		// step moves the rows by as much, from the place; a jump goes
		// where the heights known put the offset, or to the end.
		top := max(0, st.contentH-float64(st.h))
		y := max(0, min(st.scrollY, top))
		switch d := y - s.wroteY; {
		case top > 0 && y >= top-0.5:
			s.req = listRequest{set: true, end: true}
		case d >= -2*float64(st.h) && d <= 2*float64(st.h):
			s.inset += d
		default:
			s.req = listRequest{set: true, offset: true, y: y}
		}
		s.following = s.FollowEnd && top > 0 && y >= top-0.5
	}
	s.wroteY = st.scrollY
}

// anchorAt returns the row at the top of the view scrolled to y, and where
// its top goes in the list's box, as the heights known put them.
func (s *ListState) anchorAt(y float64) (int, float64) {
	a := s.heights.rowAt(max(0, y+s.border-s.padTop), s.gap)
	return a, s.padTop + s.heights.top(a, s.gap) - y
}

// find returns the row whose key is k, looking around row near.
func (s *ListState) find(k any, near, n int) (int, bool) {
	for d := 0; d <= listSearch; d++ {
		lo, hi := near-d, near+d
		if lo < 0 && hi >= n {
			break
		}
		if lo >= 0 && lo < n && s.Key(lo) == k {
			return lo, true
		}
		if d > 0 && hi >= 0 && hi < n && s.Key(hi) == k {
			return hi, true
		}
	}
	return 0, false
}

// plan returns the row the place starts from and the rows to build, as far
// as the heights known tell where the place puts them.
func (s *ListState) plan(e *Element, n int) (a, first, last int) {
	if n == 0 {
		return 0, 0, 0
	}
	view := float64(e.st.h)
	if view <= 0 {
		view = float64(e.c.h)
	}
	hs, gap := &s.heights, s.gap
	a, y, above := s.anchor, s.padTop-s.inset, 0.0
	switch r := s.req; {
	case r.set && r.offset:
		a, y = s.anchorAt(r.y)
		if r.hint {
			a, y = max(0, min(r.row, n-1)), s.padTop+hs.top(r.row, gap)-r.y
		}
	case r.set && !r.end:
		// Rows on both sides cover any alignment.
		a, y, above = max(0, min(r.row, n-1)), 0, -view
	case r.end || s.following:
		a = n - 1
		y = view - hs.height(a)
	}
	last = a
	for yy := y; last < n && yy < view && last-a < listMaxRows; last++ {
		yy += hs.height(last) + gap
	}
	first = a
	for yy := y; first > 0 && yy > above && a-first < listMaxRows; {
		first--
		yy -= hs.height(first) + gap
	}
	return a, max(0, first-listOverscan), min(n, last+listOverscan)
}

// build builds row i in the list.
func (f *listFrame) build(i int) *Element {
	c, s := f.c, f.s
	var key any = i
	if s.Key != nil {
		key = s.Key(i)
	}
	w := Box(c).Key(key).Shrink(0)
	w.listRow, w.rowIndex = true, i
	// Assistive technology sees a row of a table, an item of a list.
	if f.flat {
		w.Role(RoleRow)
	} else {
		w.Role(RoleListItem)
	}
	if sel := s.Selected; sel != nil && !f.isHeader(i) {
		t := c.theme
		w.flags |= flagClickable | flagHover | flagChoosable
		if w.Clicked() {
			f.choose(i)
			f.owner.Focus()
		}
		if w.DoubleClicked() {
			f.owner.st.submitted = true
		}
		w.Selected(*sel == i)
		if !f.flat {
			w.Radius(t.Radius)
		}
		w.styleFn = func(w *Element) {
			if w.checked != 2 && w.Hovered() {
				w.bg = t.SurfaceHover
			}
		}
	}
	w.Children(func() { f.row(i) })
	f.rows = append(f.rows, listRow{i: i, key: key, e: w})
	return w
}

// buildLate builds row i while the list lays out, as the view would have:
// with the theme it built the list with, and its input forgotten once the
// frame is laid out (layoutTree).
func (f *listFrame) buildLate(i int) *Element {
	c := f.c
	parent, theme := c.parent, c.theme
	c.parent, c.theme = f.e, f.theme
	w := f.build(i)
	c.parent, c.theme = parent, theme
	c.rt.late = true
	return w
}

func (f *listFrame) isHeader(i int) bool { return f.s.Header != nil && f.s.Header(i) }

// choose makes row i the choice, and shows it.
func (f *listFrame) choose(i int) {
	if sel := f.s.Selected; *sel != i {
		*sel = i
		f.owner.st.changed = true
		f.c.rt.consumed = true
	}
	f.s.ScrollIntoView(i)
}

// navigate moves the choice with the keys, past headers.
func (f *listFrame) navigate() {
	o, n, sel := f.owner, f.n, f.s.Selected
	if n == 0 {
		return
	}
	// step returns the first row from i on by d that is not a header.
	step := func(i, d int) int {
		for i += d; i >= 0 && i < n; i += d {
			if !f.isHeader(i) {
				return i
			}
		}
		return -1
	}
	to, moved := -1, true
	switch {
	case o.Shortcut(0, KeyDown):
		to = step(max(*sel, -1), 1)
	case o.Shortcut(0, KeyUp):
		if to = step(*sel, -1); *sel < 0 || *sel >= n {
			to = step(-1, 1)
		}
	case o.Shortcut(0, KeyHome):
		to = step(-1, 1)
	case o.Shortcut(0, KeyEnd):
		to = step(n, -1)
	case o.Shortcut(0, KeyEnter):
		if *sel >= 0 && *sel < n {
			o.st.submitted = true
		}
		moved = false
	default:
		moved = false
	}
	switch {
	case to >= 0:
		f.choose(to)
	case moved && *sel >= 0 && *sel < n:
		// Nowhere further: the choice shows again, if it scrolled away.
		f.s.ScrollIntoView(*sel)
	}
}

// focusedRow returns the row holding the keyboard focus in the last
// frame, where it is now.
func (s *ListState) focusedRow(e *Element, n int) (int, bool) {
	rt := e.c.rt
	if rt.focused == 0 || len(s.rows) == 0 {
		return 0, false
	}
	for st := rt.states[rt.focused]; st != nil && st.id != e.id && st.parent != 0; st = rt.states[st.parent] {
		if st.parent != e.id {
			continue
		}
		r, ok := s.rows[st.id]
		if !ok {
			return 0, false
		}
		i := r.row
		if s.Key != nil && (i >= n || s.Key(i) != r.key) {
			if i, ok = s.find(r.key, i, n); !ok {
				return 0, false
			}
		}
		return i, i < n
	}
	return 0, false
}

// layoutList measures and places the rows of a List w×h from its place,
// building those missing, pins the header of the section at the top, and
// sets the size of the content and the offset. Without rows, what else was
// built in the list shows, as a scroll container's content: a message that
// it is empty, say.
func (e *Element) layoutList(w, h float32) {
	f := e.list
	s, n := f.s, f.n
	hs := &s.heights
	st := e.st
	cw := max(w-e.padX(), 0)
	if cw != s.width {
		// The rows wrap anew: heights measured at another width are gone.
		hs.clear()
		s.width = cw
	}
	gap := float64(max(e.gapY, 0))
	padTop, padBottom := float64(e.contentY()), float64(e.pad[2]+e.border[2])
	H := float64(h)
	top, bottom := float64(e.border[0]), H-float64(e.border[2])
	s.gap, s.padTop, s.border = gap, padTop, top
	st.list = s
	if n == 0 {
		_, uh := boxLayout(e, cw, inf, true)
		e.contentW, e.contentH = float64(w), float64(max(uh, max(h-e.padY(), 0))+e.padY())
		layoutAbsolute(e)
		e.scrollBase = 0
		s.remember(e, 0, -1, true, st.scrollY)
		return
	}
	if f.at == nil {
		f.at = map[int]int{}
	}
	clear(f.at)
	for k := range f.rows {
		r := &f.rows[k]
		f.at[r.i] = k
		hs.set(r.i, heightAt(r.e, cw, inf))
	}
	// ensure builds and measures row i if the list lacks it.
	ensure := func(i int) bool {
		if _, ok := f.at[i]; ok {
			return true
		}
		if len(f.rows) >= listMaxRows {
			return false
		}
		r := f.buildLate(i)
		f.at[i] = len(f.rows) - 1
		hs.set(i, heightAt(r, cw, inf))
		return true
	}
	ht := hs.height

	// The row placed first, and where its top goes.
	var a int
	var ya float64
	switch req := s.req; {
	case req.end || s.following && !req.set:
		a = n - 1
		ensure(a)
		ya = H - padBottom - ht(a)
	case req.offset:
		// Where the heights known, now with the rows built measured, put
		// the offset.
		a, ya = s.anchorAt(req.y)
		if req.hint {
			a = max(0, min(req.row, n-1))
			ya = padTop + hs.top(a, gap) - req.y
		}
		ensure(a)
	case req.set:
		a = max(0, min(req.row, n-1))
		ensure(a)
		size := ht(a)
		// The header pinned over the row's section hides the top of the
		// list: the row shows below it.
		shown, start := top, padTop
		if s.Header != nil && !s.Header(a) {
			if hh := s.headerAbove(a, n); hh >= 0 && ensure(hh) {
				shown = top + ht(hh)
				start = max(start, shown)
			}
		}
		switch {
		case req.near:
			// Where the place puts it now.
			ya = padTop + hs.top(a, gap) - (hs.top(s.anchor, gap) + s.inset)
			switch {
			case ya < shown || size > bottom-shown:
				ya = start
			case ya+size > bottom:
				ya = H - padBottom - size
			}
		case req.align == Center:
			ya = (shown + bottom - size) / 2
		case req.align == End:
			ya = H - padBottom - size
		default:
			ya = start
		}
	default:
		a = s.anchor
		ensure(a)
		ya = padTop - s.inset
	}

	// Rows lo to hi are placed, row lo at yLo and row hi at yHi.
	lo, hi, yLo, yHi := a, a, ya, ya
	cover := func() {
		for hi < n-1 && yHi+ht(hi)+gap < bottom && ensure(hi+1) {
			yHi += ht(hi) + gap
			hi++
		}
		for lo > 0 && yLo > top && ensure(lo-1) {
			lo--
			yLo -= ht(lo) + gap
		}
	}
	shift := func(d float64) { yLo, yHi = yLo+d, yHi+d }
	end := func() float64 { return yHi + ht(hi) + padBottom }
	cover()
	// Keep the content's ends at the list's: the end may show above the
	// bottom after rows went away, the start below the top after rows
	// above turned out shorter than estimated.
	for k := 0; k < 4 && !(lo == 0 && hi == n-1); k++ {
		if hi == n-1 && end() < H {
			shift(H - end())
		} else if lo == 0 && yLo > padTop {
			shift(padTop - yLo)
		} else {
			break
		}
		cover()
	}
	if lo == 0 && hi == n-1 {
		switch start := yLo - padTop; {
		case end()-start <= H:
			// The rows fit: Justify places them.
			switch e.justify {
			case End:
				shift(H - end())
			case Center:
				shift((H - end() - start) / 2)
			default:
				shift(-start)
			}
		case start > 0:
			shift(-start)
		case end() < H:
			shift(H - end())
		}
	}
	for k := 0; k < listOverscan && hi < n-1 && ensure(hi+1); k++ {
		yHi += ht(hi) + gap
		hi++
	}
	for k := 0; k < listOverscan && lo > 0 && ensure(lo-1); k++ {
		lo--
		yLo -= ht(lo) + gap
	}

	// The offset: where the heights known put row lo's top, less where it
	// shows.
	content := max(padTop+padBottom+hs.top(n, gap)-gap, H)
	scroll := max(0, min(padTop+hs.top(lo, gap)-yLo, content-H))
	y := yLo
	for i := lo; i <= hi; i++ {
		f.rows[f.at[i]].y = y
		y += ht(i) + gap
	}
	for k := range f.rows {
		if r := &f.rows[k]; r.i < lo || r.i > hi {
			r.y = padTop + hs.top(r.i, gap) - scroll
		}
	}

	// The rows in view, the first of which anchors the place.
	first, last := -1, -1
	for i := lo; i <= hi; i++ {
		if y := f.rows[f.at[i]].y; y+ht(i) > top && y < bottom {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		first, last = a, a-1
	}
	inset := padTop - f.rows[f.at[first]].y
	var pinned *Element
	if s.Header != nil && last >= first {
		pinned = e.pinHeader(first, hi, top)
	}

	// Lay the rows out in order, as they show, Tab moves and assistive
	// technology reads, then what else was built above them, which lies
	// over the rows.
	var extras []*Element
	for ch := e.first; ch != nil; ch = ch.next {
		if !ch.listRow && ch.flags&flagAbsolute != 0 {
			extras = append(extras, ch)
		}
	}
	slices.SortFunc(f.rows, func(a, b listRow) int { return a.i - b.i })
	var prev *Element
	link := func(ch *Element) {
		ch.next = nil
		if prev == nil {
			e.first = ch
		} else {
			prev.next = ch
		}
		prev = ch
	}
	for k := range f.rows {
		r := &f.rows[k]
		link(r.e)
		r.e.x, r.e.y = e.contentX(), float32(r.y)
		layoutBox(r.e, cw, float32(ht(r.i)))
	}
	for _, ch := range extras {
		link(ch)
	}
	e.last = prev
	layoutAbsolute(e)
	if pinned != nil {
		// Above the rows scrolling under it, and still at the top when the
		// offset moves before placing.
		pinned.flags |= flagAbsolute
	}
	e.contentW, e.contentH = float64(w), content
	e.scrollBase = scroll
	st.scrollTo(st.scrollX, scroll)
	s.inset = inset
	s.remember(e, first, last, hi == n-1 && end() <= H+0.5, scroll)
}

// relayoutList lays the list out anew scrolled to y, as revealing its
// row ch asks, from where the heights known put ch: the rows around it
// are built, and the frame shows no gap.
func (e *Element) relayoutList(ch *Element, y float64) {
	f := e.list
	if f.n == 0 {
		e.st.scrollTo(e.st.scrollX, y)
		return
	}
	req := listRequest{set: true, offset: true, y: y}
	for _, r := range f.rows {
		if r.e == ch {
			req.row, req.hint = r.i, true
		}
	}
	f.s.req = req
	layoutBox(e, e.w, e.h)
}

// remember keeps where the list is for the next frame, and builds another
// for a view that read where it was.
func (s *ListState) remember(e *Element, first, last int, atEnd bool, scroll float64) {
	f, n := e.list, e.list.n
	if s.read && (first != s.first || last != s.last || atEnd != s.atEnd) {
		e.c.rt.animating = true
	}
	s.laidOut, s.wroteY, s.req, s.read = true, scroll, listRequest{}, false
	s.first, s.last, s.atEnd = first, last, atEnd
	s.following = s.FollowEnd && atEnd
	s.anchor, s.anchorKey = max(0, first), nil
	if n == 0 {
		s.inset = 0
	} else if s.Key != nil {
		s.anchorKey = s.Key(first)
	}
	s.selKey = nil
	if sel := s.Selected; sel != nil {
		s.selRow = *sel
		if s.Key != nil && *sel >= 0 && *sel < n {
			s.selKey = s.Key(*sel)
		}
	}
	if s.rows == nil {
		s.rows = map[uint64]listRowID{}
	}
	clear(s.rows)
	for _, r := range f.rows {
		s.rows[r.e.id] = listRowID{r.i, r.key}
	}
}

// pinHeader moves the header of the section of row first, the first in
// view, to the top of the list, below which the next header pushes it,
// and returns it if it moved.
func (e *Element) pinHeader(first, hi int, top float64) *Element {
	f := e.list
	s := f.s
	hh := s.headerAbove(first, f.n)
	if hh < 0 {
		return nil
	}
	k, ok := f.at[hh]
	if !ok {
		// Far above: build it where its section's rows are.
		f.buildLate(hh)
		k = len(f.rows) - 1
		f.at[hh] = k
		s.heights.set(hh, heightAt(f.rows[k].e, s.width, inf))
		f.rows[k].y = f.rows[f.at[first]].y - (s.heights.top(first, s.gap) - s.heights.top(hh, s.gap))
	}
	size := s.heights.height(hh)
	y := max(f.rows[k].y, top)
	for j := max(hh, first) + 1; j <= hi; j++ {
		if s.Header(j) {
			y = min(y, f.rows[f.at[j]].y-size)
			break
		}
	}
	if y == f.rows[k].y {
		return nil
	}
	f.rows[k].y = y
	return f.rows[k].e
}

// listHeader is the header found last for a row, for the next frame to
// look from there.
type listHeader struct {
	// row is the last header at or above row from, -1 for none, and the
	// rows from stop to from were looked at: none between row and from
	// is a header, and none down to stop either when row is -1.
	row, from, stop int
	ok              bool
}

// headerAbove returns the last header at or above row i, -1 for none.
func (s *ListState) headerAbove(i, n int) int {
	const far = 100_000
	hc := &s.header
	if hc.ok && (hc.row < 0 || hc.row < n && s.Header(hc.row)) {
		switch {
		case i >= hc.from && i-hc.from <= far:
			for j := i; j > hc.from; j-- {
				if s.Header(j) {
					*hc = listHeader{row: j, from: i, stop: j, ok: true}
					return j
				}
			}
			hc.from = i
			return hc.row
		case i >= hc.stop && (hc.row >= 0 || hc.stop == 0):
			return hc.row
		}
	}
	h, stop := -1, max(0, i-far)
	for j := i; j >= stop; j-- {
		if s.Header(j) {
			h, stop = j, j
			break
		}
	}
	*hc = listHeader{row: h, from: i, stop: stop, ok: true}
	return h
}

// listBlock is how many rows' heights a block of listHeights holds.
const listBlock = 64

// listHeights holds the heights measured of a list's rows, in blocks of
// listBlock rows made as rows are measured, so that its memory follows
// the rows measured rather than the rows, and estimates the others as
// their average. The offset of a row sums the totals of the blocks
// before it, summed in order once they change, and its block's heights.
type listHeights struct {
	n int
	// ids are the indexes of the blocks with measured rows, in order.
	ids    []int
	blocks []*heightBlock
	// def is the estimate while no row is measured.
	def float64
	// psum and pcount sum the heights and the counts of measured rows of
	// the blocks before each, up to date unless dirty.
	psum   []float64
	pcount []int
	dirty  bool
}

type heightBlock struct {
	h     [listBlock]float32
	known uint64
	sum   float64
}

// block returns where block b is or would go among the blocks, and
// whether it is there.
func (hs *listHeights) block(b int) (int, bool) {
	return slices.BinarySearch(hs.ids, b)
}

func (hs *listHeights) index() {
	if !hs.dirty && len(hs.psum) == len(hs.blocks)+1 {
		return
	}
	m := len(hs.blocks)
	hs.psum = slices.Grow(hs.psum[:0], m+1)[:m+1]
	hs.pcount = slices.Grow(hs.pcount[:0], m+1)[:m+1]
	hs.psum[0], hs.pcount[0] = 0, 0
	for k, blk := range hs.blocks {
		hs.psum[k+1] = hs.psum[k] + blk.sum
		hs.pcount[k+1] = hs.pcount[k] + bits.OnesCount64(blk.known)
	}
	hs.dirty = false
}

// estimate returns the height of rows not measured.
func (hs *listHeights) estimate() float64 {
	hs.index()
	if k := hs.pcount[len(hs.blocks)]; k > 0 {
		return hs.psum[len(hs.blocks)] / float64(k)
	}
	return hs.def
}

// height returns the height of row i, measured or estimated.
func (hs *listHeights) height(i int) float64 {
	if k, ok := hs.block(i / listBlock); ok {
		if blk := hs.blocks[k]; blk.known&(1<<(i%listBlock)) != 0 {
			return float64(blk.h[i%listBlock])
		}
	}
	return hs.estimate()
}

// top returns how far below the top of row 0 row i starts, with gap
// between rows.
func (hs *listHeights) top(i int, gap float64) float64 {
	i = max(0, min(i, hs.n))
	est := hs.estimate()
	b, j := i/listBlock, i%listBlock
	k, ok := hs.block(b)
	y := hs.psum[k] + float64(b*listBlock-hs.pcount[k])*est
	if ok {
		blk := hs.blocks[k]
		for at := range j {
			if blk.known&(1<<at) != 0 {
				y += float64(blk.h[at])
			} else {
				y += est
			}
		}
	} else {
		y += float64(j) * est
	}
	return y + float64(i)*gap
}

// rowAt returns the row at y below the top of row 0, with its gap.
func (hs *listHeights) rowAt(y, gap float64) int {
	if hs.n == 0 {
		return 0
	}
	lo, hi := 0, (hs.n-1)/listBlock
	for lo < hi {
		if mid := (lo + hi + 1) / 2; hs.top(mid*listBlock, gap) <= y {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	i := lo * listBlock
	end := min(hs.n, i+listBlock)
	for at := hs.top(i, gap); i < end-1; i++ {
		if at += hs.height(i) + gap; at > y {
			break
		}
	}
	return i
}

// set records that row i measured h.
func (hs *listHeights) set(i int, h float32) {
	if i < 0 || i >= hs.n {
		return
	}
	b, j := i/listBlock, i%listBlock
	k, ok := hs.block(b)
	if !ok {
		hs.ids = slices.Insert(hs.ids, k, b)
		hs.blocks = slices.Insert(hs.blocks, k, new(heightBlock))
	}
	blk := hs.blocks[k]
	bit := uint64(1) << j
	if blk.known&bit != 0 && blk.h[j] == h {
		return
	}
	blk.known |= bit
	blk.h[j] = h
	blk.total()
	hs.dirty = true
}

func (blk *heightBlock) total() {
	blk.sum = 0
	for k := range listBlock {
		if blk.known&(1<<k) != 0 {
			blk.sum += float64(blk.h[k])
		}
	}
}

// resize makes it n rows, forgetting those past them.
func (hs *listHeights) resize(n int) {
	nb := (n + listBlock - 1) / listBlock
	k, _ := hs.block(nb)
	clear(hs.blocks[k:])
	hs.ids, hs.blocks = hs.ids[:k], hs.blocks[:k]
	if k, ok := hs.block(nb - 1); ok && n%listBlock > 0 {
		blk := hs.blocks[k]
		blk.known &= 1<<(n%listBlock) - 1
		blk.total()
	}
	hs.n = n
	hs.dirty = true
}

// shift moves the heights known by d rows, as rows came or went before.
func (hs *listHeights) shift(d int) {
	ids, blocks := hs.ids, hs.blocks
	hs.ids, hs.blocks = nil, nil
	for k, blk := range blocks {
		for at := range listBlock {
			if blk.known&(1<<at) != 0 {
				hs.set(ids[k]*listBlock+at+d, blk.h[at])
			}
		}
	}
	hs.dirty = true
}

// clear forgets every height.
func (hs *listHeights) clear() {
	clear(hs.blocks)
	hs.ids, hs.blocks = hs.ids[:0], hs.blocks[:0]
	hs.dirty = true
}
