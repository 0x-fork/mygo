package ui

import (
	"fmt"
	"time"
)

// DateInput creates a field showing *date as 2006-01-02, which a calendar
// below it changes. A click, Enter or Space opens the calendar, where a
// click chooses a day, as do the arrows and Enter; Page Up and Page Down,
// or its buttons, move by months, and Escape closes it. Changed reports a
// new date, which keeps the time of day and location of *date.
func DateInput(c *Context, date *time.Time) *Element {
	t := c.theme
	b := Button(c, "")
	b.widget, b.role, b.accValue = "DateInput", RolePopUpButton, date.Format("2006-01-02")
	b.MinWidth(t.Space(32.5)).Justify(SpaceBetween)
	open := Local(b, "open", func() bool { return false })
	// cursor is the day the keys move in the calendar.
	cursor := Local(b, "cursor", func() time.Time { return *date })
	if b.Clicked() {
		*open = !*open
		*cursor = *date
	}
	b.expanded = *open
	b.Children(func() {
		Text(c, date.Format("2006-01-02")).SingleLine().FontFeatures("tnum")
		Box(c).Size(t.Space(3.5), t.Space(3.5)).Shrink(0).Draw(func(p *Painter, r Rect) {
			// A calendar page.
			p.Stroke(Rect{r.X + 1, r.Y + 2, r.W - 2, r.H - 3}, t.TextMuted, 2, 1.2)
			p.Fill(Rect{r.X + 1, r.Y + 5, r.W - 2, 1.2}, t.TextMuted, 0)
		})
	})
	choose := func(d time.Time) {
		y, m, day := d.Date()
		h, mi, s := date.Clock()
		next := time.Date(y, m, day, h, mi, s, date.Nanosecond(), date.Location())
		if !next.Equal(*date) {
			*date = next
			b.st.changed = true
			c.rt.consumed = true
		}
		*open = false
		b.Focus()
	}
	Popover(c, b, open, func() {
		cur := *cursor
		month := time.Date(cur.Year(), cur.Month(), 1, 0, 0, 0, 0, time.UTC)
		grid := Column(c).Gap(t.Space(0.5)).Focusable().AutoFocus().Role(RoleTable).Label(month.Format("January 2006"))
		grid.flags |= flagOwnRing
		move := func(d time.Time) { *cursor = d; c.rt.consumed = true }
		switch {
		case grid.Shortcut(0, KeyLeft):
			move(cur.AddDate(0, 0, -1))
		case grid.Shortcut(0, KeyRight):
			move(cur.AddDate(0, 0, 1))
		case grid.Shortcut(0, KeyUp):
			move(cur.AddDate(0, 0, -7))
		case grid.Shortcut(0, KeyDown):
			move(cur.AddDate(0, 0, 7))
		case grid.Shortcut(0, KeyPageUp):
			move(cur.AddDate(0, -1, 0))
		case grid.Shortcut(0, KeyPageDown):
			move(cur.AddDate(0, 1, 0))
		case grid.Shortcut(0, KeyEnter), grid.Shortcut(0, KeySpace):
			choose(cur)
		}
		grid.Children(func() {
			Row(c).AlignItems(Center).Gap(t.Space(1)).Children(func() {
				if Button(c, "‹").Padding(t.Space(0.5), t.Space(2.5)).Label("Previous month").Clicked() {
					move(cur.AddDate(0, -1, 0))
				}
				Text(c, month.Format("January 2006")).Bold().Grow(1).TextAlign(Center)
				if Button(c, "›").Padding(t.Space(0.5), t.Space(2.5)).Label("Next month").Clicked() {
					move(cur.AddDate(0, 1, 0))
				}
			})
			Row(c).Children(func() {
				for _, d := range []string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"} {
					Text(c, d).Width(t.Space(8)).TextAlign(Center).FontSize(t.FontSize - 2).TextColor(t.TextMuted).Role(RoleColumnHeader)
				}
			})
			// Six weeks from the Monday on or before the first.
			start := month.AddDate(0, 0, -((int(month.Weekday()) + 6) % 7))
			today := c.now
			for w := range 6 {
				Row(c).Role(RoleRow).Children(func() {
					for d := range 7 {
						day := start.AddDate(0, 0, w*7+d)
						cell := Box(c).Size(t.Space(8), t.Space(7)).Center().Radius(t.Radius).Role(RoleButton).Label(day.Format("January 2, 2006"))
						cell.flags |= flagClickable | flagHover
						if cell.Clicked() {
							choose(day)
						}
						same := func(a, b time.Time) bool {
							ay, am, ad := a.Date()
							by, bm, bd := b.Date()
							return ay == by && am == bm && ad == bd
						}
						chosen, atCursor := same(day, *date), same(day, cur)
						fg := t.Text
						if day.Month() != month.Month() {
							fg = t.TextMuted
						}
						switch {
						case chosen:
							cell.Background(t.Accent)
							fg = t.AccentText
						case atCursor:
							cell.Background(t.SurfaceHover)
						}
						if same(day, today) && !chosen {
							cell.Border(1, t.Accent)
						}
						cell.styleFn = func(cell *Element) {
							if !chosen && cell.Hovered() {
								cell.bg = t.SurfaceHover
							}
						}
						cell.checked = 1 + int8(b2f(chosen))
						cell.Children(func() {
							Text(c, fmt.Sprint(day.Day())).TextColor(fg).FontFeatures("tnum")
						})
					}
				})
			}
		})
		grid.DrawOver(func(p *Painter, r Rect) {
			if grid.FocusVisible() {
				p.FocusRing(r, [4]float32{t.Radius, t.Radius, t.Radius, t.Radius})
			}
		})
	})
	return b
}
