package ui

// TableColumn describes a column of a Table: its title, its width in DIPs, or
// 0 to share the room the others leave, and how its cells align.
type TableColumn struct {
	Title string
	Width float32
	Align Align
}

// Table creates a table of n rows under a header of columns, which only
// builds the rows in view: cell builds the content of a row's column,
// usually a Text. With selected, a click chooses a row, as Up and Down do
// while the table has the keyboard focus, and Changed reports a new
// choice; a double click or Enter on it reports Submitted:
//
//	cols := []ui.TableColumn{{Title: "Name"}, {Title: "Size", Width: 90, Align: ui.End}}
//	if ui.Table(c, cols, len(files), &app.file, func(row, col int) {
//		f := files[row]
//		switch col {
//		case 0:
//			ui.Text(c, f.Name).SingleLine()
//		case 1:
//			ui.Text(c, f.Size())
//		}
//	}).Grow(1).Submitted() {
//		app.open(files[app.file])
//	}
func Table(c *Context, columns []TableColumn, n int, selected *int, cell func(row, col int)) *Element {
	t := c.theme
	// The height of its rows.
	tableRow := t.Space(8)
	table := Column(c).Role(RoleTable).Focusable().Clip()
	table.widget = "Table"
	table.flags |= flagOwnRing
	// reveal is set when the keys moved the choice, which then scrolls
	// into view.
	reveal := Local(table, "reveal", func() bool { return false })
	if selected != nil && n > 0 {
		choose := func(i int) {
			i = max(0, min(i, n-1))
			if i != *selected {
				*selected = i
				table.st.changed = true
				c.rt.consumed = true
			}
			*reveal = true
		}
		switch {
		case table.Shortcut(0, KeyDown):
			choose(*selected + 1)
		case table.Shortcut(0, KeyUp):
			choose(*selected - 1)
		case table.Shortcut(0, KeyHome):
			choose(0)
		case table.Shortcut(0, KeyEnd):
			choose(n - 1)
		case table.Shortcut(0, KeyEnter):
			if *selected >= 0 && *selected < n {
				table.st.submitted = true
				c.rt.consumed = true
			}
		}
	}
	// cells builds a row's cells, with fill building the content of each.
	cells := func(role Role, fill func(col int)) {
		for j, col := range columns {
			box := Row(c).Padding(0, t.Space(2.5)).AlignItems(Center).Shrink(0).Clip().Role(role)
			if col.Width > 0 {
				box.Width(col.Width)
			} else {
				box.Grow(1).Shrink(1).MinWidth(0)
			}
			switch col.Align {
			case Center:
				box.Justify(Center)
			case End:
				box.Justify(End)
			}
			box.Children(func() { fill(j) })
		}
	}
	var list *Element
	table.Children(func() {
		Row(c).Height(tableRow).Shrink(0).Role(RoleRow).Children(func() {
			cells(RoleColumnHeader, func(j int) {
				Text(c, columns[j].Title).SingleLine().FontWeight(600).TextColor(t.TextMuted)
			})
		})
		Divider(c)
		list = List(c, n, tableRow, func(i int) {
			row := Row(c).Fill().Role(RoleRow)
			row.flags |= flagClickable | flagHover
			if selected != nil {
				if row.Clicked() {
					if *selected != i {
						*selected = i
						table.st.changed = true
					}
					table.Focus()
				}
				if row.DoubleClicked() {
					table.st.submitted = true
				}
				row.Selected(*selected == i)
			}
			row.styleFn = func(row *Element) {
				if row.checked != 2 && row.Hovered() {
					row.bg = t.SurfaceHover
				}
			}
			row.Children(func() {
				cells(RoleCell, func(j int) { cell(i, j) })
			})
		}).Grow(1).Role(RoleNone)
	})
	if *reveal && selected != nil {
		*reveal = false
		// The list's box is the last frame's; the next frame shows the
		// rows the new scrolling brings into view.
		st := list.st
		top, bottom := float32(*selected)*tableRow, float32(*selected+1)*tableRow
		switch {
		case top < st.scrollY:
			st.scrollY = top
			c.AnimationFrame()
		case st.h > 0 && bottom > st.scrollY+st.h:
			st.scrollY = bottom - st.h
			c.AnimationFrame()
		}
	}
	table.DrawOver(func(p *Painter, r Rect) {
		if table.FocusVisible() {
			p.FocusRing(r, [4]float32{})
		}
	})
	return table
}

// Selected shows the element as chosen among its siblings, in the accent
// color, as a row of a list, and tells assistive technology it is.
func (e *Element) Selected(on bool) *Element {
	if !on {
		e.checked = 1
		return e
	}
	t := e.c.theme
	e.checked = 2
	e.Background(t.Accent).TextColor(t.AccentText)
	return e
}
