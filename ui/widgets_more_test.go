package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

func TestTabs(t *testing.T) {
	tab := 0
	changes := 0
	tt := NewTester(func(c *Context) {
		Column(c).Padding(10).Children(func() {
			if Tabs(c, &tab, "One", "Two", "Three").Changed() {
				changes++
			}
			Textf(c, "page %d", tab)
		})
	}, 400, 200)
	if err := tt.Click("Two"); err != nil {
		t.Fatal(err)
	}
	if tab != 1 || !tt.HasText("page 1") || changes != 1 {
		t.Fatalf("a click on Two: tab %d, %d changes, texts %q", tab, changes, tt.Texts())
	}
	// The arrows move the choice, and the focus with it.
	tt.Key(0, KeyRight)
	if tab != 2 || !tt.Focused("Three") {
		t.Errorf("Right: tab %d, Three focused %v", tab, tt.Focused("Three"))
	}
	tt.Key(0, KeyRight)
	if tab != 0 || !tt.Focused("One") {
		t.Errorf("Right past the last tab: tab %d", tab)
	}
	tt.Key(0, KeyEnd)
	if tab != 2 {
		t.Errorf("End: tab %d", tab)
	}
	tt.Key(0, KeyHome)
	if tab != 0 {
		t.Errorf("Home: tab %d", tab)
	}

	tt.rt.accessibilityOn()
	tt.Frame()
	var tabs, selected int
	for _, n := range tt.h.access.Nodes {
		if n.Role == platform.RoleTab {
			tabs++
			if n.States&platform.AccessChecked != 0 {
				selected++
			}
		}
	}
	if tabs != 3 || selected != 1 {
		t.Errorf("assistive technology sees %d tabs, %d selected", tabs, selected)
	}
}

func TestSplit(t *testing.T) {
	size := float32(120)
	tt := NewTester(func(c *Context) {
		Split(c, &size, func() { Text(c, "left") }, func() { Text(c, "right") }).Fill()
	}, 400, 200)
	if r, _ := tt.Find("right"); r.X < 120+6-0.5 || r.X > 120+6+0.5 {
		t.Fatalf("the second pane starts at %v", r.X)
	}
	// The divider moves with the pointer, within the window.
	x, y := float32(123), float32(100)
	tt.Press(x, y)
	tt.Move(x+50, y)
	tt.Release(x+50, y)
	if size != 170 {
		t.Errorf("dragged 50 DIPs right, the first pane is %v wide", size)
	}
	tt.Press(x+50, y)
	tt.Move(0, y)
	tt.Release(0, y)
	if size != 40 {
		t.Errorf("dragged to the edge, the first pane is %v wide", size)
	}
	// The arrows move it once it has the focus.
	tt.ClickAt(43, y)
	tt.Key(0, KeyRight)
	if size != 50 {
		t.Errorf("Right on the divider: %v", size)
	}
	if r, _ := tt.Find("right"); r.X != 56 {
		t.Errorf("the second pane follows to %v", r.X)
	}
}

func TestNumberInput(t *testing.T) {
	v := 5.0
	tt := NewTester(func(c *Context) {
		Column(c).Padding(10).AlignItems(Start).Children(func() {
			NumberInput(c, &v, 0, 10, 0.5).Label("Count")
		})
	}, 400, 100)
	if err := tt.Click("Increase"); err != nil {
		t.Fatal(err)
	}
	tt.Click("Decrease")
	tt.Click("Decrease")
	if v != 4.5 {
		t.Errorf("one step up and two down from 5: %v", v)
	}
	r, _ := tt.Find("Count")
	tt.ClickAt(r.X+20, r.Y+r.H/2)
	tt.Key(Cmd, KeyA)
	tt.Type("7.5")
	if v != 7.5 {
		t.Errorf("typed 7.5: %v", v)
	}
	tt.Key(Cmd, KeyA)
	tt.Type("12")
	if v != 7.5 {
		t.Errorf("12 is out of range, yet the value is %v", v)
	}
	tt.Key(0, KeyUp)
	if v != 8 {
		t.Errorf("Up from 7.5: %v", v)
	}
	for range 5 {
		tt.Key(0, KeyUp)
	}
	if v != 10 {
		t.Errorf("Up past the top: %v", v)
	}
}

func TestToast(t *testing.T) {
	tt := NewTester(func(c *Context) {
		if Button(c, "Save").Clicked() {
			c.Toast("Saved")
		}
	}, 400, 300)
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Saved") {
		t.Fatalf("no toast after the click: %q", tt.Texts())
	}
	tt.Click("Save")
	if len(tt.rt.toasts) != 1 {
		t.Errorf("the same message shows %d times", len(tt.rt.toasts))
	}
	tt.rt.toasts[0].at = time.Now().Add(-toastTime)
	tt.Frame()
	if tt.HasText("Saved") {
		t.Error("the toast stays once its time is over")
	}
}

func TestTable(t *testing.T) {
	names := make([]string, 100)
	for i := range names {
		names[i] = "file" + string(rune('A'+i%26)) + string(rune('0'+i/26))
	}
	sel, opened := -1, -1
	cols := []TableColumn{{Title: "Name"}, {Title: "Size", Width: 80, Align: End}}
	tt := NewTester(func(c *Context) {
		Column(c).Fill().Padding(10).Children(func() {
			if Table(c, cols, len(names), &sel, func(row, col int) {
				if col == 0 {
					Text(c, names[row])
				} else {
					Textf(c, "%d KB", row)
				}
			}).Grow(1).Submitted() {
				opened = sel
			}
		})
	}, 400, 300)
	if !tt.HasText("Name") || !tt.HasText("fileA0") || tt.HasText(names[99]) {
		t.Fatalf("texts %q: the header and the first rows, not the last", tt.Texts())
	}
	if err := tt.Click("fileC0"); err != nil {
		t.Fatal(err)
	}
	if sel != 2 {
		t.Errorf("clicked the third row: selected %d", sel)
	}
	tt.Key(0, KeyDown)
	tt.Key(0, KeyDown)
	if sel != 4 {
		t.Errorf("Down twice from the third row: %d", sel)
	}
	tt.Key(0, KeyEnd)
	if sel != 99 || !tt.HasText(names[99]) {
		t.Errorf("End: selected %d, last row shown %v", sel, tt.HasText(names[99]))
	}
	tt.Key(0, KeyEnter)
	if opened != 99 {
		t.Errorf("Enter opened %d", opened)
	}
	tt.Key(0, KeyHome)
	r, _ := tt.Find("fileA0")
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if sel != 0 || opened != 0 {
		t.Errorf("a double click on the first row: selected %d, opened %d", sel, opened)
	}
}

func TestTree(t *testing.T) {
	srcOpen, cmdOpen := false, true
	var clicked string
	tt := NewTester(func(c *Context) {
		Tree(c, func() {
			item := func(label string, open *bool, children func()) {
				if TreeItem(c, label, open, children).Selected(clicked == label).Clicked() {
					clicked = label
				}
			}
			item("src", &srcOpen, func() {
				item("main.go", nil, nil)
				item("cmd", &cmdOpen, func() {
					item("tool.go", nil, nil)
				})
			})
			item("go.mod", nil, nil)
		})
	}, 400, 300)
	if tt.HasText("main.go") || !tt.HasText("go.mod") {
		t.Fatalf("a closed folder shows its items: %q", tt.Texts())
	}
	// Its arrow opens it.
	r, _ := tt.Find("src")
	tt.ClickAt(r.X-10, r.Y+r.H/2)
	if !srcOpen || !tt.HasText("main.go") || !tt.HasText("tool.go") || clicked != "" {
		t.Fatalf("after a click on the arrow: open %v, clicked %q, texts %q", srcOpen, clicked, tt.Texts())
	}
	// Nested items are indented.
	if m, _ := tt.Find("main.go"); m.X <= r.X {
		t.Errorf("main.go at %v, src at %v", m.X, r.X)
	}
	tt.Click("src")
	if clicked != "src" {
		t.Errorf("clicked %q", clicked)
	}
	// The arrows move the focus through the items in view.
	tt.Key(0, KeyDown)
	if !tt.Focused("main.go") {
		t.Error("Down from src does not focus main.go")
	}
	tt.Key(0, KeyDown)
	tt.Key(0, KeyLeft) // closes cmd
	if cmdOpen || tt.HasText("tool.go") {
		t.Errorf("Left on the open cmd leaves it open %v", cmdOpen)
	}
	tt.Key(0, KeyLeft) // to its parent
	if !tt.Focused("src") {
		t.Error("Left on the closed cmd does not focus src")
	}
	tt.Key(0, KeyEnter)
	if clicked != "src" {
		t.Errorf("Enter chose %q", clicked)
	}
	tt.Key(0, KeyLeft)
	if srcOpen || tt.HasText("main.go") {
		t.Error("Left on src does not close it")
	}
}

func TestDateInput(t *testing.T) {
	date := time.Date(2026, 10, 3, 9, 30, 0, 0, time.UTC)
	changes := 0
	tt := NewTester(func(c *Context) {
		Column(c).Padding(10).AlignItems(Start).Children(func() {
			if DateInput(c, &date).Label("Due").Changed() {
				changes++
			}
		})
	}, 400, 420)
	if !tt.HasText("2026-10-03") {
		t.Fatalf("texts %q", tt.Texts())
	}
	if err := tt.Click("Due"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("October 2026") {
		t.Fatalf("no calendar after a click: %q", tt.Texts())
	}
	if err := tt.Click("October 15, 2026"); err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 15, 9, 30, 0, 0, time.UTC); !date.Equal(want) || changes != 1 || tt.HasText("October 2026") {
		t.Fatalf("a click on the 15th: %v, %d changes, calendar shown %v", date, changes, tt.HasText("October 2026"))
	}
	// From the keyboard: Enter opens it, the arrows and Page Down move,
	// Enter chooses.
	tt.Key(0, KeyEnter)
	tt.Key(0, KeyRight)
	tt.Key(0, KeyDown)
	tt.Key(0, KeyPageDown)
	tt.Key(0, KeyEnter)
	if want := time.Date(2026, 11, 23, 9, 30, 0, 0, time.UTC); !date.Equal(want) {
		t.Errorf("Right, Down and Page Down from the 15th chose %v", date)
	}
	tt.Click("Due")
	tt.Click("Next month")
	if !tt.HasText("December 2026") {
		t.Errorf("Next month shows %q", tt.Texts())
	}
	tt.Key(0, KeyEscape)
	if tt.HasText("December 2026") {
		t.Error("Escape leaves the calendar open")
	}
}
