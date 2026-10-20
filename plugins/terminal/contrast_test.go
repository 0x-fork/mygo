package terminal

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestContrasted(t *testing.T) {
	white, black := ui.RGB(255, 255, 255), ui.RGB(0, 0, 0)
	for _, c := range []struct {
		fg, bg ui.Color
		ratio  float32
	}{
		{ui.RGB(240, 240, 240), white, 3},  // a dark theme's gray on a light one
		{ui.RGB(30, 30, 30), black, 3},     // a light theme's on a dark one
		{ui.RGB(229, 229, 16), white, 4.5}, // bright yellow
	} {
		got := contrasted(c.fg, c.bg, c.ratio)
		r := contrast(luminance(got), luminance(c.bg))
		if r < c.ratio || r > c.ratio*1.1 {
			t.Errorf("contrasted(%v, %v, %v) = %v, a ratio of %v", c.fg, c.bg, c.ratio, got, r)
		}
	}
	if got := contrasted(black, white, 4.5); got != black {
		t.Errorf("black on white changed to %v", got)
	}
	// Short of the ratio even at black or white, the text takes the one
	// that contrasts more.
	if got := contrasted(ui.RGB(100, 100, 100), ui.RGB(128, 128, 128), 21); got != black {
		t.Errorf("gray on gray at 21 = %v, want black", got)
	}
}
