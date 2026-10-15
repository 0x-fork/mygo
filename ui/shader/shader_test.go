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

var _ ui.Effect = shader.Effect{}

// Effects draw an element from its content: a ColorEffect changes its
// colors, a DistortionEffect moves its pixels, transparent beyond what
// renders, and a LayerEffect reads it anywhere; the window around the
// element keeps its own pixels.
func TestEffects(t *testing.T) {
	invert := shader.MustCompile(`package main

func ColorEffect(pos vec2, color vec4) vec4 {
	return vec4(color.a-color.rgb, color.a)
}
`)
	shift := shader.MustCompile(`package main

var By float

func DistortionEffect(pos vec2) vec2 {
	return pos - vec2(By, 0)
}
`)
	mirror := shader.MustCompile(`package main

func LayerEffect(pos vec2) vec4 {
	return layerAt(vec2(rectSize().x-pos.x, pos.y))
}
`)
	// A box red on its left half and green on its right.
	halves := func(c *ui.Context, fx shader.Effect) {
		ui.Row(c).Width(80).Height(40).Effect(fx).Children(func() {
			ui.Box(c).Width(40).Height(40).Background(ui.RGB(255, 0, 0))
			ui.Box(c).Width(40).Height(40).Background(ui.RGB(0, 255, 0))
		})
	}
	view := func(c *ui.Context) {
		ui.Column(c).Fill().Background(ui.RGB(255, 255, 255)).Gap(10).Padding(10).Children(func() {
			halves(c, invert.Effect(shader.Options{}))
			halves(c, shift.Effect(shader.Options{Uniforms: map[string]any{"By": 20}, MaxSampleOffset: ui.Point{X: 20}}))
			halves(c, mirror.Effect(shader.Options{}))
		})
	}
	tt := ui.NewTester(view, 200, 200)
	img := tt.Image()
	at := func(x, y int) [4]uint8 {
		o := img.PixOffset(x, y)
		return [4]uint8(img.Pix[o : o+4])
	}
	red, green, white := [4]uint8{255, 0, 0, 255}, [4]uint8{0, 255, 0, 255}, [4]uint8{255, 255, 255, 255}
	cyan, magenta := [4]uint8{0, 255, 255, 255}, [4]uint8{255, 0, 255, 255}
	for _, c := range []struct {
		name string
		x, y int
		want [4]uint8
	}{
		{"inverted left", 20, 30, cyan},
		{"inverted right", 70, 30, magenta},
		{"beside the inverted box", 120, 30, white},
		{"shifted: transparent where nothing moved in", 15, 80, white},
		{"shifted: red moved right", 45, 80, red},
		{"shifted: green", 75, 80, green},
		{"mirrored left", 20, 130, green},
		{"mirrored right", 70, 130, red},
	} {
		if got := at(c.x, c.y); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// An effect draws a transformed element as transformed: its content in
// the element's own space, the effect over its box.
func TestEffectTransformed(t *testing.T) {
	invert := shader.MustCompile(`package main

func ColorEffect(pos vec2, color vec4) vec4 {
	return vec4(color.a-color.rgb, color.a)
}
`)
	view := func(c *ui.Context) {
		ui.Box(c).Fill().Background(ui.RGB(255, 255, 255)).Padding(50).Children(func() {
			ui.Box(c).Width(100).Height(20).Background(ui.RGB(255, 0, 0)).Rotate(90).
				Effect(invert.Effect(shader.Options{}))
		})
	}
	img := ui.NewTester(view, 200, 200).Image()
	at := func(x, y int) [4]uint8 {
		o := img.PixOffset(x, y)
		return [4]uint8(img.Pix[o : o+4])
	}
	// Rotated about its center (100, 60): upright, 20 wide, 100 tall.
	if got := at(100, 25); got != [4]uint8{0, 255, 255, 255} {
		t.Errorf("inside the rotated box: %v, want cyan", got)
	}
	if got := at(140, 60); got != [4]uint8{255, 255, 255, 255} {
		t.Errorf("where the box was before rotating: %v, want white", got)
	}
}

// A shader has one entry point, of a kind that decides how it draws; an
// effect's leaves image3 to its content.
func TestKinds(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"package main\nfunc Fragment() vec4 { return vec4(1) }\nfunc LayerEffect(pos vec2) vec4 { return vec4(1) }\n", "one entry point"},
		{"package main\nfunc LayerEffect(pos vec2) vec4 { return image3At(pos) }\n", "image3 is its content"},
	} {
		if _, err := shader.Compile(c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Compile(%q) = %v, want an error with %q", c.src, err, c.want)
		}
	}
	effect := shader.MustCompile("package main\nfunc LayerEffect(pos vec2) vec4 { return layerAt(pos) }\n")
	fill := shader.MustCompile("package main\nfunc Fragment() vec4 { return vec4(1) }\n")
	mustPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s did not panic", name)
			}
		}()
		fn()
	}
	mustPanic("Effect of a Fragment shader", func() { fill.Effect(shader.Options{}) })
	view := func(c *ui.Context) {
		ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
			mustPanic("Draw of an effect's shader", func() { effect.Draw(p, r, shader.Options{}) })
		})
	}
	ui.NewTester(view, 10, 10).Image()
}
