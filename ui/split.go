package ui

// Split lays out first and second side by side, with a divider between
// them that the user drags to resize them, or moves with the arrows once
// it has the keyboard focus: *size is the width of first, which the
// divider keeps 40 DIPs from either edge. Changed reports a move.
//
//	ui.Split(c, &app.sidebar, app.files, app.editor).Fill()
func Split(c *Context, size *float32, first, second func()) *Element {
	return split(c, size, first, second, false)
}

// SplitVertical is Split with first above second, *size its height.
func SplitVertical(c *Context, size *float32, first, second func()) *Element {
	return split(c, size, first, second, true)
}

func split(c *Context, size *float32, first, second func(), vertical bool) *Element {
	t := c.theme
	minPane, grip := t.space(10), t.space(1.5)
	e := Row(c).AlignItems(Stretch)
	if vertical {
		e = Column(c).AlignItems(Stretch)
	}
	e.widget = "Split"
	// The room to share, as the last frame laid it out.
	total := e.st.w
	if vertical {
		total = e.st.h
	}
	set := func(v float32) {
		if total > 0 {
			v = min(v, total-grip-minPane)
		}
		v = max(v, minPane)
		if v != *size {
			*size = v
			e.st.changed = true
			c.rt.consumed = true
		}
	}
	e.Children(func() {
		a := Box(c).Shrink(0).Clip()
		if vertical {
			a.Height(*size)
		} else {
			a.Width(*size)
		}
		a.Children(first)

		div := Box(c).Shrink(0).Focusable().Role(RoleSplitter).Label("Divider")
		div.widget = "Divider"
		div.flags |= flagDraggable | flagHover | flagOwnRing
		cursor := CursorResizeEW
		if vertical {
			div.Height(grip)
			cursor = CursorResizeNS
		} else {
			div.Width(grip)
		}
		div.Cursor(cursor)
		if dx, dy, ok := div.Dragged(); ok {
			if vertical {
				set(*size + dy)
			} else {
				set(*size + dx)
			}
		}
		back, forth := KeyLeft, KeyRight
		if vertical {
			back, forth = KeyUp, KeyDown
		}
		if div.Shortcut(0, back) {
			set(*size - t.space(2.5))
		}
		if div.Shortcut(0, forth) {
			set(*size + t.space(2.5))
		}
		div.hasRange, div.accRange = true, [3]float64{float64(minPane), float64(max(total-grip-minPane, minPane)), float64(*size)}
		div.Draw(func(p *Painter, r Rect) {
			line := t.Border
			if div.Hovered() || div.Pressed() {
				line = t.Accent
			}
			if vertical {
				p.Fill(Rect{r.X, r.Y + r.H/2 - 0.5, r.W, 1}, line, 0)
			} else {
				p.Fill(Rect{r.X + r.W/2 - 0.5, r.Y, 1, r.H}, line, 0)
			}
			if div.FocusVisible() {
				p.FocusRing(r, [4]float32{})
			}
		})

		b := Box(c).Grow(1).Shrink(1).Clip()
		if vertical {
			b.MinHeight(0)
		} else {
			b.MinWidth(0)
		}
		b.Children(second)
	})
	return e
}
