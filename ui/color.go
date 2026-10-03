package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/internal/scene"
)

// Color is an sRGB color with straight (not premultiplied) alpha.
type Color struct{ R, G, B, A uint8 }

// Transparent is the color of nothing.
var Transparent = Color{}

// RGB returns an opaque color.
func RGB(r, g, b uint8) Color { return Color{r, g, b, 255} }

// RGBA returns a color with alpha between 0 and 1.
func RGBA(r, g, b uint8, alpha float32) Color { return Color{r, g, b, alphaByte(alpha)} }

// Hex parses "#rgb", "#rgba", "#rrggbb" or "#rrggbbaa". It panics on other
// input, which is a mistake in the program.
func Hex(s string) Color {
	c, err := parseHex(s)
	if err != nil {
		panic(err)
	}
	return c
}

func parseHex(s string) (Color, error) {
	h := strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(h) == 3 || len(h) == 4 {
		var b strings.Builder
		for _, c := range h {
			b.WriteRune(c)
			b.WriteRune(c)
		}
		h = b.String()
	}
	if len(h) == 6 {
		h += "ff"
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if len(h) != 8 || err != nil {
		return Color{}, fmt.Errorf("ui: invalid color %q", s)
	}
	return Color{uint8(v >> 24), uint8(v >> 16), uint8(v >> 8), uint8(v)}, nil
}

func alphaByte(a float32) uint8 {
	if a <= 0 {
		return 0
	}
	if a >= 1 {
		return 255
	}
	return uint8(a*255 + 0.5)
}

// Alpha returns the color with its alpha multiplied by a.
func (c Color) Alpha(a float32) Color {
	c.A = alphaByte(float32(c.A) / 255 * a)
	return c
}

// gray returns the color in shades of gray, of the same luminance.
func (c Color) gray() Color {
	l := uint8(0.2126*float32(c.R) + 0.7152*float32(c.G) + 0.0722*float32(c.B) + 0.5)
	return Color{l, l, l, c.A}
}

// Mix returns the color t of the way from c to o.
func (c Color) Mix(o Color, t float32) Color {
	t = max(0, min(t, 1))
	m := func(a, b uint8) uint8 { return uint8(float32(a)*(1-t) + float32(b)*t + 0.5) }
	return Color{m(c.R, o.R), m(c.G, o.G), m(c.B, o.B), m(c.A, o.A)}
}

// Over returns c composited over the opaque color base.
func (c Color) Over(base Color) Color {
	a := float32(c.A) / 255
	o := c.Mix(base, 1-a)
	o.A = base.A
	return o
}

func (c Color) scene() scene.Color { return scene.Color{R: c.R, G: c.G, B: c.B, A: c.A} }
