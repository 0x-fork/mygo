// Package gputest has what the tests of the GPU renderers share: a scene
// that draws every kind of operation, and a comparison of what a renderer
// drew with what the CPU renderer draws, which the renderers must match.
package gputest

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/internal/raster"
	"github.com/egoist/mygo/internal/scene"
)

// Scene returns a 320×240 scene with fills, borders, gradients, shadows,
// nested rounded clips, glyphs from both atlases and images.
func Scene() *scene.Scene {
	s := &scene.Scene{Width: 320, Height: 240, Clear: scene.Color{R: 246, G: 247, B: 249, A: 255}}
	mask := scene.NewAtlas(1, 64, 64)
	color := scene.NewAtlas(4, 32, 32)
	s.MaskAtlas, s.ColorAtlas = mask, color
	// A disc and a ring as glyph masks, and a color glyph.
	disc := make([]byte, 16*16)
	ring := make([]byte, 12*12)
	for y := range 16 {
		for x := range 16 {
			dx, dy := float64(x)-7.5, float64(y)-7.5
			disc[y*16+x] = byte(255 * clamp(7.5-math.Sqrt(dx*dx+dy*dy)))
			if x < 12 && y < 12 {
				ex, ey := float64(x)-5.5, float64(y)-5.5
				d := math.Sqrt(ex*ex + ey*ey)
				ring[y*12+x] = byte(255 * clamp(1.5-math.Abs(d-4)))
			}
		}
	}
	dx, dy, _ := mask.Alloc(16, 16)
	mask.Put(dx, dy, 16, 16, disc, 16)
	rx, ry, _ := mask.Alloc(12, 12)
	mask.Put(rx, ry, 12, 12, ring, 12)
	emoji := make([]byte, 10*10*4)
	for i := 0; i < len(emoji); i += 4 {
		k := i / 4
		// Premultiplied: half transparent in the corner rows.
		a := byte(255)
		if k < 10 || k >= 90 {
			a = 128
		}
		emoji[i], emoji[i+1], emoji[i+2], emoji[i+3] = byte(uint16(240)*uint16(a)/255), byte(uint16(160)*uint16(a)/255), byte(uint16(20)*uint16(a)/255), a
	}
	cx, cy, _ := color.Alloc(10, 10)
	color.Put(cx, cy, 10, 10, emoji, 40)
	pix := make([]byte, 8*8*4)
	for y := range 8 {
		for x := range 8 {
			i := (y*8 + x) * 4
			pix[i], pix[i+1], pix[i+2], pix[i+3] = byte(x*32), byte(y*32), 160, 255
		}
	}
	img := scene.NewImageRGBA(8, 8, pix)

	red := scene.Color{R: 220, G: 40, B: 40, A: 255}
	blue := scene.Color{R: 37, G: 99, B: 235, A: 255}
	ink := scene.Color{R: 20, G: 24, B: 32, A: 255}
	r4 := func(r float32) [4]float32 { return [4]float32{r, r, r, r} }
	add := func(op scene.Op) { s.Ops = append(s.Ops, op) }
	add(scene.Op{Kind: scene.OpShadow, Rect: scene.Rect{X: 20, Y: 24, W: 120, H: 80}, Radii: r4(12), Color: scene.Color{A: 90}, Blur: 16})
	add(scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 20, Y: 20, W: 120, H: 80}, Radii: r4(12), Color: scene.Color{R: 255, G: 255, B: 255, A: 255},
		Border: 1, BorderColor: scene.Color{R: 200, G: 204, B: 210, A: 255}})
	add(scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 160.5, Y: 20.25, W: 140, H: 30}, Radii: [4]float32{15, 4, 15, 4},
		Color: red, Color2: blue, HasGrad: true, Gradient: [4]float32{160, 20, 300, 50}})
	add(scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 170, Y: 60, W: 60, H: 40}, Color: scene.Color{R: 37, G: 99, B: 235, A: 128}, Border: 3, BorderColor: red, Radii: r4(8)})
	add(scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 240, Y: 60, W: 40, H: 40}, Radii: r4(20), Color: blue, Opacity: 0.5})
	add(scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 20, Y: 120, W: 200, H: 100}, Radii: r4(16)})
	add(scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 0, Y: 110, W: 320, H: 130}, Color: scene.Color{R: 230, G: 236, B: 250, A: 255}})
	add(scene.Op{Kind: scene.OpPushClip, Rect: scene.Rect{X: 100, Y: 130, W: 200, H: 70}})
	add(scene.Op{Kind: scene.OpShadow, Rect: scene.Rect{X: 120, Y: 150, W: 60, H: 30}, Radii: r4(6), Color: scene.Color{B: 80, A: 140}, Blur: 10})
	add(scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: 90, Y: 140, W: 50, H: 50}, Color: red, Radii: r4(4)})
	add(scene.Op{Kind: scene.OpPopClip})
	start := int32(len(s.Glyphs))
	for i := range 6 {
		s.Glyphs = append(s.Glyphs, scene.Glyph{X: float32(30 + i*20), Y: 200, W: 16, H: 16, U: uint16(dx), V: uint16(dy), UW: 16, VH: 16, Color: ink},
			scene.Glyph{X: float32(32 + i*20), Y: 182, W: 12, H: 12, U: uint16(rx), V: uint16(ry), UW: 12, VH: 12, Color: blue})
	}
	s.Glyphs = append(s.Glyphs, scene.Glyph{X: 160, Y: 180, W: 10, H: 10, U: uint16(cx), V: uint16(cy), UW: 10, VH: 10, Color: scene.Color{A: 255}, Colored: true})
	add(scene.Op{Kind: scene.OpGlyphs, Start: start, End: int32(len(s.Glyphs))})
	add(scene.Op{Kind: scene.OpPopClip})
	add(scene.Op{Kind: scene.OpImage, Rect: scene.Rect{X: 240, Y: 120, W: 8, H: 8}, Image: img, Src: scene.Rect{W: 8, H: 8}})
	add(scene.Op{Kind: scene.OpImage, Rect: scene.Rect{X: 240, Y: 140, W: 64, H: 64}, Radii: r4(10), Image: img, Src: scene.Rect{W: 8, H: 8}})
	return s
}

// Compare compares pixels a renderer drew, premultiplied BGRA rows of
// stride bytes, with what the CPU renderer draws of s. Antialiased edges
// and interpolated images may differ a little; anything else fails t. With
// MYGO_GPU_PNG naming a directory, it saves both images there.
func Compare(t testing.TB, name string, got []byte, stride int, s *scene.Scene) {
	t.Helper()
	want := raster.NewImage(s.Width, s.Height)
	raster.Render(want, s)
	worst, off := 0, 0
	worstAt := image.Point{}
	for y := range s.Height {
		for x := range s.Width {
			g := got[y*stride+4*x:][:4]
			w := want.Pix[y*want.Stride+4*x:][:4]
			d := 0
			for c := range 4 {
				d = max(d, absInt(int(g[c])-int(w[c])))
			}
			if d > worst {
				worst, worstAt = d, image.Pt(x, y)
			}
			if d > 2 {
				off++
			}
		}
	}
	if dir := os.Getenv("MYGO_GPU_PNG"); dir != "" {
		g := &raster.Image{W: s.Width, H: s.Height, Stride: stride, Pix: got}
		save(t, filepath.Join(dir, name+".png"), g)
		save(t, filepath.Join(dir, name+"-cpu.png"), want)
	}
	// One pixel in two hundred may differ by more than 2 of 255, none by
	// more than 24.
	if worst > 24 || off*200 > s.Width*s.Height {
		t.Errorf("%s: %d pixels differ from the CPU renderer's by more than 2, the most by %d at %v", name, off, worst, worstAt)
	}
}

func save(t testing.TB, path string, m *raster.Image) {
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img := &image.RGBA{Pix: m.RGBA(), Stride: 4 * m.W, Rect: image.Rect(0, 0, m.W, m.H)}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(fmt.Errorf("%s: %w", path, err))
	}
}

func clamp(v float64) float64 { return max(0, min(1, v)) }

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
