package ui

import (
	"math"
	"strings"
)

// GridState is the state of a GridView: how its items are told apart and
// chosen, as a List's rows are, and the place of its rows.
type GridState struct {
	// Key returns an identity for item i, as ListState.Key does for a
	// row: the choice and the items' state follow it.
	Key func(item int) any
	// Selected, when set, lets a click choose an item, as the arrows do
	// while the grid has the keyboard focus: *Selected is the item chosen,
	// -1 for none. The grid's Changed reports a new choice, and its
	// Submitted a double click or Enter.
	Selected *int
	// Selection, when set, lets the user choose several items, held by
	// their keys, as a List's Selection: Cmd-click (Ctrl-click on Linux
	// and Windows) adds an item or takes it out, Shift-click and Shift
	// with the arrows choose those from the item last chosen, and Cmd+A
	// chooses all. Selected, set as well, is the item last chosen.
	Selection Selector
	// Label returns the text of item i: typing its first letters while
	// the grid has the focus chooses it, and assistive technology reads it
	// as the item's name.
	Label func(item int) string

	rows ListState
	// cols is how many columns the items took in the last frame, and pivot
	// the item Shift chooses from.
	cols, pivot int
	started     bool
}

// ScrollTo scrolls the grid to show item at align, as ListState.ScrollTo
// does a row.
func (s *GridState) ScrollTo(item int, align Align) {
	s.rows.ScrollTo(item/max(s.cols, 1), align)
}

// ScrollIntoView scrolls the grid as little as shows item.
func (s *GridState) ScrollIntoView(item int) {
	s.rows.ScrollIntoView(item / max(s.cols, 1))
}

// gridFit is how many columns a grid's items took, which its layout checks
// against the width its rows have: items at least minW wide, gap apart.
type gridFit struct {
	cols      int
	minW, gap float32
}

// key returns the key of item i.
func (s *GridState) key(i int) any {
	if s.Key != nil {
		return s.Key(i)
	}
	return i
}

// GridView creates a scroll container of n items in a grid, as Photos'
// library and Finder's icon view, built with item(i): as many columns as
// fit items at least minWidth wide, which share the width left, and rows
// height high, built only as they show, so grids of millions of items are
// as fast as small ones. Gap spaces the items.
//
// s keeps the place and says how items are chosen, or nil: with
// s.Selected, a click chooses an item, as the arrow keys do in two
// dimensions while the grid has the keyboard focus, with Home, End and the
// first letters of an item's Label.
//
//	app.photos.Selected = &app.photo
//	ui.GridView(c, &app.photos, len(photos), 140, 120, func(i int) {
//		ui.Image(c, thumbs[i]).Fit(ui.Contain).Grow(1)
//	}).Grow(1)
func GridView(c *Context, s *GridState, n int, minWidth, height float32, item func(i int)) *Element {
	t := c.theme
	e := Scroll(c)
	e.widget, e.role = "GridView", RoleList
	if s == nil {
		s = Local(e, "grid", func() GridState { return GridState{} })
	}
	n = max(n, 0)
	gap := t.Space(2)
	// The columns: those that fit the width the rows had in the last
	// frame, or the window's, first. The layout builds the next frame
	// again where they changed.
	width := s.rows.width
	if width <= 0 {
		width = c.w
	}
	cols := max(1, int(math.Floor(float64((width+gap)/(minWidth+gap)))))
	if s.cols != cols && s.cols > 0 && s.started {
		// Keep the rows in view on the same items.
		first, _ := s.rows.Visible()
		s.rows.ScrollTo(first*s.cols/cols, Start)
	}
	s.cols, s.started = cols, true
	rows := (n + cols - 1) / cols
	if s.Selected != nil {
		e.Focusable()
		e.choosesItems = true
		if s.Label != nil {
			e.flags |= flagTypeSelect
		}
	}
	e.setSize = n
	e.chooseMany = s.Selection != nil
	var cursor *Element
	buildList(c, e, e, &s.rows, rows, func(r int) {
		row := Row(c).Gap(gap).PaddingY(gap / 2).AlignItems(Stretch).Role(RoleNone)
		row.Children(func() {
			for i := r * cols; i < min(n, (r+1)*cols); i++ {
				if cell := s.cell(c, e, i, n, height, item); s.Selected != nil && i == *s.Selected {
					cursor = cell
				}
			}
			// Empty places keep the last row's items as wide as the others.
			for k := n; k < (r+1)*cols && r == rows-1; k++ {
				Box(c).Grow(1).Basis(0).MinWidth(0)
			}
		})
	}, gridList)
	// The list's own state is the rows': the grid takes the keys and tells
	// assistive technology about its items.
	e.rowsOf = nil
	e.gridFit = &gridFit{cols: cols, minW: minWidth, gap: gap}
	s.keys(e, n)
	e.activeDescendant = cursor
	return e
}

// cell builds item i of a grid, choosing it when clicked.
func (s *GridState) cell(c *Context, grid *Element, i, n int, height float32, item func(i int)) *Element {
	t := c.theme
	// A border, clear unless chosen, keeps the content in place as it is.
	cell := Column(c).Key(s.key(i)).Grow(1).Basis(0).MinWidth(0).Height(height).Radius(t.Radius).Border(2, Color{}).Role(RoleListItem)
	cell.setPos, cell.setSize = i+1, n
	if s.Label != nil {
		cell.Label(s.Label(i))
	}
	if s.Selected != nil {
		cell.flags |= flagClickable | flagHover | flagChoosable
		if cell.Clicked() {
			s.click(grid, i, cell.ClickModifiers())
			grid.Focus()
		}
		if cell.DoubleClicked() {
			grid.st.submitted = true
		}
		chosen := s.chosen(i)
		cell.checked = 1 + int8(b2f(chosen))
		switch {
		case chosen && grid.Focused():
			cell.Background(t.Accent.Alpha(0.2)).Border(2, t.Accent)
		case chosen:
			cell.Background(t.SurfacePressed).Border(2, t.Border)
		default:
			cell.styleFn = func(cell *Element) {
				if cell.Hovered() {
					cell.bg = t.SurfaceHover
				}
			}
		}
		if i == *s.Selected && s.Selection != nil && !chosen {
			// Where the keys moved without choosing.
			cell.DrawOver(func(p *Painter, r Rect) {
				if grid.FocusVisible() {
					p.FocusRing(Rect{r.X + 3, r.Y + 3, r.W - 6, r.H - 6}, cell.radius)
				}
			})
		}
	}
	cell.Children(func() { item(i) })
	return cell
}

// chosen reports whether item i is chosen.
func (s *GridState) chosen(i int) bool {
	if s.Selection != nil {
		return s.Selection.has(s.key(i))
	}
	return s.Selected != nil && *s.Selected == i
}

// click chooses item i as a click with mods does.
func (s *GridState) click(grid *Element, i int, mods Modifiers) {
	switch {
	case s.Selection == nil:
		s.choose(grid, i)
	case mods&Shift != 0:
		s.extend(grid, i)
	case mods&Cmd != 0:
		s.Selection.set(s.key(i), !s.Selection.has(s.key(i)))
		*s.Selected, s.pivot = i, i
		grid.st.changed = true
	default:
		s.choose(grid, i)
	}
}

// choose chooses item i alone.
func (s *GridState) choose(grid *Element, i int) {
	changed := *s.Selected != i
	if sel := s.Selection; sel != nil {
		if sel.size() != 1 || !sel.has(s.key(i)) {
			sel.clear()
			sel.set(s.key(i), true)
			changed = true
		}
	}
	*s.Selected, s.pivot = i, i
	if changed {
		grid.st.changed = true
	}
}

// extend chooses the items from the pivot to i.
func (s *GridState) extend(grid *Element, i int) {
	p := s.pivot
	if p < 0 || s.Selection == nil {
		s.choose(grid, i)
		return
	}
	s.Selection.clear()
	for j := min(p, i); j <= max(p, i); j++ {
		s.Selection.set(s.key(j), true)
	}
	*s.Selected = i
	grid.st.changed = true
}

// keys moves the choice with the arrows in two dimensions, Home, End and
// the first letters of an item, while the grid has the focus.
func (s *GridState) keys(grid *Element, n int) {
	if s.Selected == nil || n == 0 {
		return
	}
	cols := max(s.cols, 1)
	at := *s.Selected
	in := at >= 0 && at < n
	moves := []struct {
		key Key
		d   int
	}{{KeyRight, 1}, {KeyLeft, -1}, {KeyDown, cols}, {KeyUp, -cols}}
	to, extend := -1, false
	for _, m := range moves {
		plain, shift := grid.Shortcut(0, m.key), s.Selection != nil && grid.Shortcut(Shift, m.key)
		if !plain && !shift {
			continue
		}
		extend = shift
		switch {
		case !in:
			to = 0
		case at+m.d >= 0 && at+m.d < n:
			to = at + m.d
		case m.d == cols && at/cols < (n-1)/cols:
			to = n - 1 // the last row, shorter
		}
	}
	if grid.Shortcut(0, KeyHome) {
		to = 0
	}
	if grid.Shortcut(0, KeyEnd) {
		to = n - 1
	}
	if s.Selection != nil && grid.Shortcut(Cmd, KeyA) {
		for j := range n {
			s.Selection.set(s.key(j), true)
		}
		grid.st.changed = true
	}
	if grid.Shortcut(0, KeyEnter) && in {
		grid.st.submitted = true
	}
	if st := grid.st; st.typing && st.typed != "" && s.Label != nil {
		for k := 1; k <= n; k++ {
			j := (max(at, 0) + k - b2i(len([]rune(st.typed)) > 1) + n) % n
			if strings.HasPrefix(strings.ToLower(s.Label(j)), strings.ToLower(st.typed)) {
				to = j
				break
			}
		}
	}
	if to < 0 {
		return
	}
	if extend {
		if s.pivot < 0 || !in {
			s.pivot = max(at, 0)
		}
		s.extend(grid, to)
	} else {
		s.choose(grid, to)
	}
	s.ScrollIntoView(to)
	grid.c.rt.consumed = true
}
