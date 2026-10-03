package ui

import (
	"math"
	"testing"
)

// BenchmarkFrame builds, lays out, paints and renders on the CPU a whole
// frame of a 980×720 window at twice the density, as on macOS and Linux.
func BenchmarkFrame(b *testing.B) {
	d := &demo{choice: "a", size: "Medium", volume: 40}
	tt := NewTester(d.view, 980, 720)
	tt.SetScale(2)
	for b.Loop() {
		tt.h.img.Invalidate()
		tt.Frame()
	}
}

// BenchmarkFrameHover renders the frames of the pointer going over a
// button and off it: the CPU renderer redraws the button alone.
func BenchmarkFrameHover(b *testing.B) {
	d := &demo{choice: "a", size: "Medium", volume: 40}
	tt := NewTester(d.view, 980, 720)
	tt.SetScale(2)
	r, _ := tt.Find("Increment")
	on := false
	for b.Loop() {
		if on = !on; on {
			tt.Move(r.X+5, r.Y+5)
		} else {
			tt.Move(r.X-5, r.Y-5)
		}
	}
}

// BenchmarkFrameAnimatedPaths renders the frames of a card drawn with a
// path that changes every frame, as the gallery's Drawing page: a stroked
// wave, whose mask is drawn anew each frame, and a disc, which is cached.
func BenchmarkFrameAnimatedPaths(b *testing.B) {
	frame := 0
	view := func(c *Context) {
		frame++
		phase := float64(frame) * 0.05
		Box(c).Size(700, 320).Draw(func(p *Painter, r Rect) {
			var wave Path
			for i := 0; i <= 100; i++ {
				x := r.X + 20 + float32(i)*6
				y := r.Y + r.H/2 + float32(60*math.Sin(phase+float64(i)/12))
				if i == 0 {
					wave.MoveTo(x, y)
				} else {
					wave.LineTo(x, y)
				}
			}
			p.StrokePath(&wave, 3, RGB(220, 40, 40))
			var dot Path
			dot.Circle(r.X+r.W-70, r.Y+70, 36)
			p.FillPath(&dot, RGB(37, 99, 235))
		})
	}
	tt := NewTester(view, 980, 720)
	tt.SetScale(2)
	b.ReportAllocs()
	for b.Loop() {
		tt.Frame()
	}
}
