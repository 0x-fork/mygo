package gpu

import (
	"testing"

	"github.com/egoist/mygo/internal/scene"
)

func TestBuild(t *testing.T) {
	red := scene.Color{R: 255, A: 255}
	atlas := scene.NewAtlas(1, 64, 32)
	img1 := scene.NewImageRGBA(2, 2, make([]byte, 16))
	img2 := scene.NewImageRGBA(2, 2, make([]byte, 16))
	s := &scene.Scene{Width: 200, Height: 100, MaskAtlas: atlas}
	s.Glyphs = []scene.Glyph{{X: 10.4, Y: 20.6, W: 8, H: 9, U: 16, V: 8, UW: 8, VH: 9, Color: red}}
	s.Ops = []scene.Op{
		{Kind: scene.OpFill, Rect: scene.Rect{X: 1, Y: 2, W: 30, H: 20}, Radii: [4]float32{50, 50, 50, 50}, Color: red, Border: scene.Uniform(2), BorderColor: red},
		{Kind: scene.OpShadow, Rect: scene.Rect{X: 5, Y: 5, W: 10, H: 10}, Color: red, Blur: 8},
		{Kind: scene.OpPushClip, Rect: scene.Rect{X: 10.5, Y: 10, W: 100, H: 50}, Radii: [4]float32{6, 6, 6, 6}},
		{Kind: scene.OpGlyphs, Start: 0, End: 1},
		{Kind: scene.OpImage, Rect: scene.Rect{W: 4, H: 4}, Image: img1, Src: scene.Rect{W: 2, H: 2}},
		{Kind: scene.OpImage, Rect: scene.Rect{W: 4, H: 4}, Image: img2, Src: scene.Rect{W: 1, H: 2}},
		{Kind: scene.OpPushClip, Rect: scene.Rect{X: 500, Y: 500, W: 10, H: 10}},
		{Kind: scene.OpFill, Rect: scene.Rect{X: 500, Y: 500, W: 5, H: 5}, Color: red},
		{Kind: scene.OpPopClip},
		{Kind: scene.OpPopClip},
		{Kind: scene.OpFill, Rect: scene.Rect{X: 0, Y: 0, W: 0, H: 5}, Color: red},
		{Kind: scene.OpShadow, Rect: scene.Rect{X: 5, Y: 5, W: 10, H: 10}, Color: red, Blur: 0.5, Opacity: 0.5},
	}
	textures := map[*scene.Image]uintptr{img1: 1, img2: 2}
	var b Builder
	b.Build(s, func(img *scene.Image) uintptr { return textures[img] })

	if len(b.Instances) != 6 {
		t.Fatalf("%d instances", len(b.Instances))
	}
	fill, shadow, glyph, im1, im2, hard := b.Instances[0], b.Instances[1], b.Instances[2], b.Instances[3], b.Instances[4], b.Instances[5]
	if fill.Params != [4]float32{0, 0, 0, 1} || fill.UV != [4]float32{2, 2, 2, 2} || fill.Radii != [4]float32{10, 10, 10, 10} || fill.Inner != [4]float32{8, 8, 8, 8} {
		t.Errorf("fill %+v", fill)
	}
	if fill.Clip[2] < 1e5 || fill.Color != [4]float32{1, 0, 0, 1} {
		t.Errorf("fill without a clip: %+v", fill)
	}
	if shadow.Params != [4]float32{1, 0, 4, 1} {
		t.Errorf("shadow %+v", shadow.Params)
	}
	if glyph.Rect != [4]float32{10, 21, 8, 9} || glyph.UV != [4]float32{0.25, 0.25, 0.375, 0.53125} || glyph.Params[0] != 2 {
		t.Errorf("glyph %+v", glyph)
	}
	if glyph.Clip != [4]float32{10.5, 10, 100, 50} || glyph.ClipRadii != [4]float32{6, 6, 6, 6} {
		t.Errorf("glyph clip %v %v", glyph.Clip, glyph.ClipRadii)
	}
	if im1.Params[0] != 4 || im2.UV != [4]float32{0, 0, 0.5, 1} {
		t.Errorf("images %+v %+v", im1, im2)
	}
	if hard.Params != [4]float32{0, 0, 0, 0.5} {
		t.Errorf("a shadow without blur draws as a fill: %+v", hard.Params)
	}
	want := []Batch{
		{Start: 0, Count: 2, Scissor: Scissor{0, 0, 200, 100}},
		{Start: 2, Count: 2, Scissor: Scissor{10, 10, 111, 60}, Image: 1},
		{Start: 4, Count: 1, Scissor: Scissor{10, 10, 111, 60}, Image: 2},
		{Start: 5, Count: 1, Scissor: Scissor{0, 0, 200, 100}},
	}
	if len(b.Batches) != len(want) {
		t.Fatalf("batches %+v", b.Batches)
	}
	for i := range want {
		if b.Batches[i] != want[i] {
			t.Errorf("batch %d: %+v, want %+v", i, b.Batches[i], want[i])
		}
	}

	// Building again reuses the memory and starts over.
	b.Build(&scene.Scene{Width: 10, Height: 10}, nil)
	if len(b.Instances) != 0 || len(b.Batches) != 0 {
		t.Errorf("an empty scene left %d instances", len(b.Instances))
	}
}

func TestSourceSum(t *testing.T) {
	lf, crlf := "float4 ps() {\n\treturn 1;\n}\n", "float4 ps() {\r\n\treturn 1;\r\n}\r\n"
	if SourceSum(lf) != SourceSum(crlf) {
		t.Error("a checkout with CRLF line endings has another sum")
	}
	if SourceSum(lf) == SourceSum(lf+" ") || len(SourceSum(lf)) != 64 {
		t.Error("the sum does not tell sources apart")
	}
}
