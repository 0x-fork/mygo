package shader_test

import (
	"image"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// A shader fills its rectangle, rounded, with what it computes from its
// uniforms, a position in DIPs and a bitmap; the window shows it.
func TestDraw(t *testing.T) {
	s := shader.MustCompile(`package main

var Color vec4
var Split float

func Fragment(dst vec4, pos vec2) vec4 {
	if pos.x < Split {
		return Color
	}
	return image0At(vec2(0))
}
`)
	red := ui.NewBitmap(solid(2, 2, 255, 0, 0))
	view := func(c *ui.Context) {
		ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
			s.Draw(p, ui.Rect{X: 10, Y: 10, W: 100, H: 40}, shader.Options{
				Uniforms: map[string]any{"Color": []float32{0, 0, 1, 1}, "Split": 50, "Unknown": 1},
				Images:   [4]*ui.Bitmap{red},
				Radius:   8,
			})
		})
	}
	tt := ui.NewTester(view, 200, 100)
	tt.SetScale(2)
	img := tt.Image()
	at := func(x, y int) [4]uint8 {
		o := img.PixOffset(x, y)
		return [4]uint8(img.Pix[o : o+4])
	}
	if got := at(2*30, 2*30); got != [4]uint8{0, 0, 255, 255} {
		t.Errorf("left of Split: %v, want blue", got)
	}
	if got := at(2*90, 2*30); got != [4]uint8{255, 0, 0, 255} {
		t.Errorf("right of Split: %v, want the bitmap's red", got)
	}
	if got := at(2*10+1, 2*10+1); got != at(2, 2) {
		t.Errorf("the rounded corner shows %v, want the background", got)
	}
}

// Compile reports where a shader is wrong; Draw panics on a uniform set to
// a value of no number.
func TestErrors(t *testing.T) {
	_, err := shader.Compile("package main\n\nfunc Fragment(dst vec4, pos vec2) vec4 {\n\treturn 1 + true\n}\n")
	if err == nil || !strings.Contains(err.Error(), "4:") {
		t.Errorf("error %v, want one at line 4", err)
	}
	if _, err := shader.Compile("package main\n"); err == nil {
		t.Error("a shader without Fragment compiled")
	}
	s := shader.MustCompile("package main\n\nvar X float\n\nfunc Fragment(dst vec4, pos vec2) vec4 {\n\treturn vec4(X)\n}\n")
	defer func() {
		if recover() == nil {
			t.Error("a uniform set to a string drew")
		}
	}()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
			s.Draw(p, r, shader.Options{Uniforms: map[string]any{"X": "1"}})
		})
	}, 50, 50)
	tt.Frame()
}

func solid(w, h int, r, g, b uint8) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = r, g, b, 255
	}
	return img
}
