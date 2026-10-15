package shader

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/shaderir"
)

// testSource reads two images, one exactly and one sampled, and uniforms
// of each kind, as the renderers must alike.
const testSource = `package main

var Seed uint
var Tint vec4
var Count int
var Spots [3]vec3
var Turn mat2

// Bools last: Metal's take a byte each, at the end of its struct.
var On, Off, Third bool

func pcg(v uint) uint {
	s := v*747796405 + 2891336453
	w := ((s >> ((s >> 28) + 4)) ^ s) * 277803737
	return (w >> 22) ^ w
}

func Fragment(dst vec4, pos vec2) vec4 {
	q := Turn * (pos - rectSize()/2)
	glow := 0.0
	for i := 0; i < Count; i++ {
		d := distance(q, Spots[i].xy)
		glow += exp(-d*d/(Spots[i].z*Spots[i].z))
	}
	h := float(pcg(uint(int(pos.x/4))*0x9E3779B9^pcg(uint(int(pos.y/4))^Seed))>>8) / 16777216.0
	a := image0At(pos * pixelScale() / 2)
	b := image1Sample(pos / rectSize() * image1Size())
	c := mix(a, b, 0.5)*0.6 + Tint*clamp(glow, 0, 1)*0.3 + vec4(h*0.1)
	if On && !Off && Third {
		c.g += 0.1
	}
	return c
}
`

// testScene returns a scene of s over a gray frame at scale 2, reading a
// gradient bitmap and a canvas of three rectangles, which differs top to
// bottom and left to right, as a renderer keeping its rows going up must
// show it.
func testScene(s *Shader) *scene.Scene {
	const scale = 2
	grad := image.NewRGBA(image.Rect(0, 0, 64, 40))
	for y := range 40 {
		for x := range 64 {
			grad.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 6), 128, 255})
		}
	}
	canvas := scene.NewCanvas(20, 12)
	canvas.Scene.Ops = []scene.Op{
		{Kind: scene.OpFill, Rect: scene.Rect{W: 10, H: 6}, Color: scene.Color{R: 220, G: 40, B: 40, A: 255}},
		{Kind: scene.OpFill, Rect: scene.Rect{X: 10, W: 10, H: 6}, Color: scene.Color{R: 37, G: 99, B: 235, A: 255}},
		{Kind: scene.OpFill, Rect: scene.Rect{Y: 6, W: 20, H: 6}, Color: scene.Color{R: 30, G: 160, B: 60, A: 255}},
	}
	images := [4]*scene.Image{scene.NewImage(grad), canvas}

	r := scene.Rect{X: 8, Y: 6, W: 224, H: 108}
	words := make([]uint32, s.words)
	for i, img := range images {
		if img != nil {
			words[2*i], words[2*i+1] = math.Float32bits(float32(img.W)), math.Float32bits(float32(img.H))
		}
	}
	words[scaleWord-2], words[scaleWord-1] = math.Float32bits(r.W/scale), math.Float32bits(r.H/scale)
	words[scaleWord] = math.Float32bits(scale)
	values := map[string]any{
		"Seed":  uint32(0x3fa1c2d9),
		"Tint":  []float32{1, 0.8, 0.2, 1},
		"Count": 2,
		"Spots": []float32{-20, 0, 18, 25, 10, 12, 0, 0, 1},
		"Turn":  []float32{0.8, 0.6, -0.6, 0.8},
		"On":    true,
		"Off":   false,
		"Third": true,
	}
	for name, v := range values {
		u := s.uniforms[name]
		kind := u.t.Scalar()
		if u.t.Main == shaderir.Array {
			kind = u.t.Sub[0].Scalar()
		}
		set(words[u.word:u.word+u.n], kind, name, v)
	}
	sc := &scene.Scene{Width: 240, Height: 120, Clear: scene.Color{R: 240, G: 240, B: 240, A: 255}}
	sc.Ops = append(sc.Ops, scene.Op{Kind: scene.OpEffect, Rect: r, Radii: [4]float32{16, 16, 16, 16}})
	sc.Effects = append(sc.Effects, scene.EffectOp{Effect: s.fx, Uniforms: words, Images: images})
	return sc
}

// dfdySource is green where y grows downward on the screen, as the
// position's does, and red elsewhere: GPUs differ in which way their
// window's y goes.
const dfdySource = `package main

func Fragment(dst vec4, pos vec2) vec4 {
	if dfdy(pos.y) > 0 {
		return vec4(0, 1, 0, 1)
	}
	return vec4(1, 0, 0, 1)
}
`

// dfdyScene returns a scene of dfdySource in a canvas, and the canvas
// shown, as renderers flip canvases: dfdy must be positive in both.
func dfdyScene() *scene.Scene {
	s := MustCompile(dfdySource)
	words := make([]uint32, s.words)
	words[scaleWord] = math.Float32bits(1)
	canvas := scene.NewCanvas(16, 16)
	canvas.Scene.Ops = []scene.Op{{Kind: scene.OpEffect, Rect: scene.Rect{W: 16, H: 16}}}
	canvas.Scene.Effects = []scene.EffectOp{{Effect: s.fx, Uniforms: words}}
	sc := &scene.Scene{Width: 40, Height: 20}
	sc.Ops = []scene.Op{
		{Kind: scene.OpEffect, Rect: scene.Rect{W: 20, H: 20}},
		{Kind: scene.OpImage, Rect: scene.Rect{X: 22, Y: 2, W: 16, H: 16}, Image: canvas, Src: scene.Rect{W: 16, H: 16}},
	}
	sc.Effects = []scene.EffectOp{{Effect: s.fx, Uniforms: words}}
	sc.Canvases = []*scene.Image{canvas}
	return sc
}

// checkDfdy checks that the window's pixel (10, 10) and the canvas's
// (30, 10) are green, in BGRA or RGBA rows of stride bytes.
func checkDfdy(t *testing.T, pix []byte, stride int) {
	t.Helper()
	for _, x := range []int{10, 30} {
		p := pix[10*stride+4*x:][:4]
		if p[1] < 200 || p[0] > 50 && p[2] > 50 {
			t.Errorf("pixel (%d, 10) is %v, want green: dfdy goes the wrong way", x, p)
		}
	}
}

// boolSource reads only bools besides the bridge's uniforms, which leaves
// Metal's struct aligned to 8, the bools in its last bytes.
const boolSource = `package main

var A, B, C bool

func Fragment(dst vec4, pos vec2) vec4 {
	v := vec4(0, 0, 0, 1)
	if A {
		v.r = 1
	}
	if B {
		v.g = 1
	}
	if C {
		v.b = 1
	}
	return v
}
`

// boolScene returns a scene of boolSource with A, B and C true: white.
func boolScene() *scene.Scene {
	s := MustCompile(boolSource)
	words := make([]uint32, s.words)
	words[scaleWord] = math.Float32bits(1)
	for _, name := range []string{"A", "B", "C"} {
		set(words[s.uniforms[name].word:][:1], shaderir.Bool, name, true)
	}
	sc := &scene.Scene{Width: 20, Height: 20}
	sc.Ops = []scene.Op{{Kind: scene.OpEffect, Rect: scene.Rect{W: 20, H: 20}}}
	sc.Effects = []scene.EffectOp{{Effect: s.fx, Uniforms: words}}
	return sc
}
