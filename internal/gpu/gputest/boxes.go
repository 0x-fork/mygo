package gputest

import "github.com/egoist/mygo/internal/scene"

// BoxesScene returns a w×h frame of n rounded boxes of 24 pixels, each
// casting a shadow, in rows of 30, as an animation of boxes moving draws
// them: under translations (Element.Translate) when translated, else at
// the same places without transforms. The places fall between pixels.
func BoxesScene(w, h, n int, translated bool) *scene.Scene {
	scene.FoldTranslations() // as package ui does
	s := &scene.Scene{Width: w, Height: h, Clear: scene.Color{R: 246, G: 247, B: 249, A: 255}}
	for i := range n {
		x, y := 8+float32(i%30)*40+float32(i%7)*0.25, 8+float32(i/30)*38+float32(i%5)*0.2
		m := scene.Translation(x, y)
		box := scene.Rect{W: 24, H: 24}
		if !translated {
			m, box.X, box.Y = scene.Affine{}, x, y
		}
		cast := box
		cast.Y += 2
		c := scene.Color{R: uint8(40 + i*7%180), G: uint8(80 + i*13%150), B: uint8(120 + i*3%120), A: 255}
		s.Ops = append(s.Ops,
			scene.Op{Kind: scene.OpShadow, Rect: cast, Radii: scene.Uniform(6), Color: scene.Color{A: 70}, Blur: 8, Transform: m},
			scene.Op{Kind: scene.OpFill, Rect: box, Radii: scene.Uniform(6), Color: c, Transform: m})
	}
	return s
}
