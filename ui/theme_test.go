package ui

import "testing"

func TestSpacingScalesWidgets(t *testing.T) {
	type sizes struct{ button, check, tab Rect }
	measure := func(spacing float32) sizes {
		tt := NewTester(func(c *Context) {
			th := *c.Theme()
			th.Spacing = spacing
			c.SetTheme(&th)
			Column(c).AlignItems(Start).Children(func() {
				Button(c, "OK").Label("button")
				on, tab := false, 0
				Checkbox(c, &on, "").Label("check")
				Tabs(c, &tab, "One", "Two")
			})
		}, 400, 300)
		var s sizes
		s.button, _ = tt.Find("button")
		s.check, _ = tt.Find("check")
		s.tab, _ = tt.Find("One")
		return s
	}
	compact, normal, roomy, unset := measure(3), measure(4), measure(5), measure(0)
	if normal.check.W != 16 || normal.check.H != 16 {
		t.Errorf("the check box is %vx%v at the default spacing, not 16x16", normal.check.W, normal.check.H)
	}
	if unset != normal {
		t.Errorf("no spacing lays widgets out as %+v, not as the default %+v", unset, normal)
	}
	for name, got := range map[string][3]Rect{
		"button":    {compact.button, normal.button, roomy.button},
		"check box": {compact.check, normal.check, roomy.check},
	} {
		if !(got[0].W < got[1].W && got[1].W < got[2].W && got[0].H < got[1].H && got[1].H < got[2].H) {
			t.Errorf("the %s does not grow with the spacing: %v", name, got)
		}
	}
	// The text of a tab sits in its padding: 3 units to the left.
	for _, s := range []sizes{compact, normal, roomy} {
		if s.tab.W <= 0 {
			t.Fatal("no tab")
		}
	}
	if !(compact.tab.X < normal.tab.X && normal.tab.X < roomy.tab.X) {
		t.Errorf("the tabs' padding does not follow the spacing: %v, %v, %v", compact.tab.X, normal.tab.X, roomy.tab.X)
	}
}
