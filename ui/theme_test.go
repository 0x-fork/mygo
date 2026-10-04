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

func TestParseHex(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Color
		ok   bool
	}{
		{"#2563eb", Color{0x25, 0x63, 0xeb, 255}, true},
		{"2563EB", Color{0x25, 0x63, 0xeb, 255}, true},
		{" #00ff0080 ", Color{0, 255, 0, 0x80}, true},
		{"#fA0", Color{255, 0xaa, 0, 255}, true},
		{"#fa08", Color{255, 0xaa, 0, 0x88}, true},
		{"#12345", Color{}, false},
		{"#1234567", Color{}, false},
		{"#123456789", Color{}, false},
		{"#12345g", Color{}, false},
		{"#+12345", Color{}, false},
		{"#é12", Color{}, false},
		{"", Color{}, false},
	} {
		got, err := parseHex(tc.in)
		if got != tc.want || (err == nil) != tc.ok {
			t.Errorf("parseHex(%q) = %v, %v; want %v, ok %v", tc.in, got, err, tc.want, tc.ok)
		}
	}
	if n := testing.AllocsPerRun(10, func() { Hex("#2563eb") }); n != 0 {
		t.Errorf("Hex allocates %v times", n)
	}
}
