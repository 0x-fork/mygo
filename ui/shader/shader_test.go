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

// FillPath, StrokePath and Text paint shapes with a shader in place of a
// color, its positions across the shape's box; the rest of the window
// keeps its own pixels.
func TestFill(t *testing.T) {
	s := shader.MustCompile(`package main

func Fragment(dst vec4, pos vec2) vec4 {
	if pos.x < rectSize().x/2 {
		return vec4(1, 0, 0, 1)
	}
	return vec4(0, 0, 1, 1)
}
`)
	var circle ui.Path
	circle.Circle(50, 50, 40)
	var line ui.Path
	line.MoveTo(110, 20).LineTo(190, 20)
	var textW float32
	view := func(c *ui.Context) {
		ui.Box(c).Fill().Background(ui.RGB(255, 255, 255)).Draw(func(p *ui.Painter, r ui.Rect) {
			s.FillPath(p, &circle, shader.Options{})
			s.StrokePath(p, &line, 8, shader.Options{})
			s.Text(p, 110, 50, "MMMM", 24, shader.Options{})
			textW, _ = p.MeasureText(0, ui.Span{Text: "MMMM", Size: 24})
		})
	}
	tt := ui.NewTester(view, 200, 100)
	img := tt.Image()
	at := func(x, y int) [4]uint8 {
		o := img.PixOffset(x, y)
		return [4]uint8(img.Pix[o : o+4])
	}
	red, blue, white := [4]uint8{255, 0, 0, 255}, [4]uint8{0, 0, 255, 255}, [4]uint8{255, 255, 255, 255}
	for _, c := range []struct {
		name string
		x, y int
		want [4]uint8
	}{
		{"the circle's left", 30, 50, red},
		{"the circle's right", 70, 50, blue},
		{"outside the circle", 15, 15, white},
		{"the stroke's left", 120, 20, red},
		{"the stroke's right", 180, 20, blue},
		{"above the stroke", 150, 12, white},
	} {
		if got := at(c.x, c.y); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// The text's ink: reddish on the left of its box, bluish on the right.
	var left, right [3]int
	mid := 110 + int(textW/2)
	for y := 50; y < 85; y++ {
		for x := 110; x < 110+int(textW) && x < 200; x++ {
			p := at(x, y)
			if p == white {
				continue
			}
			if p[1] > p[0] || p[1] > p[2] {
				t.Fatalf("text at (%d, %d) is %v, not the shader's colors", x, y, p)
			}
			if x < mid-2 {
				left[0] += int(p[0]) - int(p[2])
			} else if x > mid+2 {
				right[0] += int(p[2]) - int(p[0])
			}
		}
	}
	if left[0] <= 0 || right[0] <= 0 {
		t.Errorf("text not filled across its box: left %d, right %d", left[0], right[0])
	}
}
