package raster

import (
	"image"
	"math"
	"runtime"

	"github.com/egoist/mygo/internal/scene"
)

// Renderer draws the successive scenes of a window into Image, redrawing
// only where a scene differs from the one before: a blinking caret, a
// ticking clock or a button under the pointer redraws only itself.
//
// It compares the operations of the two scenes from both ends, so a change
// in the middle of the display list redraws the boxes of the operations
// that changed, clipped as they draw. Glyphs whose pixels changed in an
// atlas, and images whose pixels changed, count as changed.
type Renderer struct {
	// Image holds the last scene drawn, in mem.
	Image Image
	mem   *pixels

	r      renderer
	valid  bool
	clear  scene.Color
	ops    []scene.Op
	glyphs []scene.Glyph
	// bounds is where each operation of the last scene drew; versions has
	// the versions of its images.
	bounds, next []image.Rectangle
	versions     []uint64
	mask, color  atlasMark
	damage       []image.Rectangle
	clips        []image.Rectangle
}

// atlasMark is the state of an atlas a scene drew from.
type atlasMark struct {
	atlas        *scene.Atlas
	gen, version uint64
}

// changes returns the rectangles of a that changed since the mark, or
// false when everything may have.
func (m *atlasMark) changes(a *scene.Atlas) ([]image.Rectangle, bool) {
	if a == nil || m.atlas == nil {
		return nil, a == m.atlas
	}
	if a != m.atlas {
		return nil, false
	}
	rects, full := a.Changes(m.gen, m.version)
	return rects, !full
}

func (m *atlasMark) set(a *scene.Atlas) {
	m.atlas = a
	if a != nil {
		m.gen, m.version = a.Generation(), a.Version()
	}
}

// Render draws s, and returns the rectangles of Image it changed. s may
// change once Render returns.
func (r *Renderer) Render(s *scene.Scene) []image.Rectangle {
	if r.whole(s) {
		r.resize(s.Width, s.Height)
		r.damage = append(r.damage[:0], image.Rect(0, 0, s.Width, s.Height))
	}
	for _, d := range r.damage {
		r.r.render(&r.Image, s, d)
	}
	r.remember(s)
	return r.damage
}

// pixels is the memory of a Renderer's image (allocPixels).
type pixels struct {
	b       []byte
	mapped  bool
	cleanup runtime.Cleanup
}

// resize makes Image w×h, reusing its memory when it can.
func (r *Renderer) resize(w, h int) {
	n := 4 * w * h
	if r.mem == nil || len(r.mem.b) < n {
		r.mem.free()
		r.mem = allocPixels(n)
	}
	r.Image = Image{W: w, H: h, Stride: 4 * w, Pix: r.mem.b[:n]}
}

// Release frees the image and what Render remembers: the next Render
// draws everything. Image must not be used meanwhile.
func (r *Renderer) Release() {
	r.mem.free()
	*r = Renderer{}
}

// Changes returns how many pixels Render would draw for s.
func (r *Renderer) Changes(s *scene.Scene) int {
	if r.whole(s) {
		return s.Width * s.Height
	}
	area := 0
	for _, d := range r.damage {
		area += d.Dx() * d.Dy()
	}
	return area
}

// whole sets the damage of s against the last scene, and reports whether
// s must be drawn whole.
func (r *Renderer) whole(s *scene.Scene) bool {
	r.next = r.opBounds(s, r.next[:0])
	return !r.valid || s.Width != r.Image.W || s.Height != r.Image.H || s.Clear != r.clear || !r.diff(s)
}

// Invalidate makes the next Render draw everything.
func (r *Renderer) Invalidate() { r.valid = false }

// diff sets the damage between the last scene and s, and reports false
// when s must be drawn whole.
func (r *Renderer) diff(s *scene.Scene) bool {
	maskRects, ok := r.mask.changes(s.MaskAtlas)
	if !ok {
		return false
	}
	colorRects, ok := r.color.changes(s.ColorAtlas)
	if !ok {
		return false
	}
	same := func(i, j int) bool { return r.same(i, s, j, maskRects, colorRects) }
	n, m := len(r.ops), len(s.Ops)
	pre := 0
	for pre < n && pre < m && same(pre, pre) {
		pre++
	}
	suf := 0
	for suf < n-pre && suf < m-pre && same(n-1-suf, m-1-suf) {
		suf++
	}
	r.damage = r.damage[:0]
	if n == m {
		for k := pre; k < n-suf; k++ {
			if !same(k, k) {
				r.add(r.bounds[k])
				r.add(r.next[k])
			}
		}
	} else {
		for i := pre; i < n-suf; i++ {
			r.add(r.bounds[i])
		}
		for j := pre; j < m-suf; j++ {
			r.add(r.next[j])
		}
	}
	area := 0
	for _, d := range r.damage {
		area += d.Dx() * d.Dy()
	}
	// Past half the window, drawing it whole costs about the same.
	return area*2 < s.Width*s.Height
}

// same reports whether operation i of the last scene draws what operation
// j of s does.
func (r *Renderer) same(i int, s *scene.Scene, j int, maskRects, colorRects []image.Rectangle) bool {
	a, b := r.ops[i], s.Ops[j]
	ga, gb := r.glyphs[a.Start:a.End], s.Glyphs[b.Start:b.End]
	a.Start, a.End, b.Start, b.End = 0, 0, 0, 0
	if a != b {
		return false
	}
	switch b.Kind {
	case scene.OpGlyphs:
		if len(ga) != len(gb) {
			return false
		}
		for k := range gb {
			if ga[k] != gb[k] {
				return false
			}
			g := &gb[k]
			rects := maskRects
			if g.Colored {
				rects = colorRects
			}
			at := image.Rect(int(g.U), int(g.V), int(g.U)+int(g.UW), int(g.V)+int(g.VH))
			for _, c := range rects {
				if c.Overlaps(at) {
					return false
				}
			}
		}
	case scene.OpImage:
		if b.Image != nil && b.Image.Version() != r.versions[i] {
			return false
		}
	}
	return true
}

// add adds a rectangle to the damage, merged with one it touches.
func (r *Renderer) add(b image.Rectangle) {
	if b.Empty() {
		return
	}
	for i, d := range r.damage {
		if d.Inset(-8).Overlaps(b) {
			r.damage[i] = d.Union(b)
			return
		}
	}
	r.damage = append(r.damage, b)
	if len(r.damage) > 8 {
		u := r.damage[0]
		for _, d := range r.damage[1:] {
			u = u.Union(d)
		}
		r.damage = append(r.damage[:0], u)
	}
}

// opBounds appends where each operation of s draws, within the clips
// around it.
func (r *Renderer) opBounds(s *scene.Scene, out []image.Rectangle) []image.Rectangle {
	clip := image.Rect(0, 0, s.Width, s.Height)
	r.clips = r.clips[:0]
	for i := range s.Ops {
		op := &s.Ops[i]
		var b image.Rectangle
		switch op.Kind {
		case scene.OpFill, scene.OpImage:
			b = outset(op.Rect, 1)
		case scene.OpShadow:
			b = outset(op.Rect, 1.5*op.Blur+1)
		case scene.OpGlyphs:
			for _, g := range s.Glyphs[op.Start:op.End] {
				b = b.Union(outset(scene.Rect{X: g.X, Y: g.Y, W: g.W, H: g.H}, 1))
			}
		case scene.OpPushClip:
			// A clip that changes changes everything it cuts.
			b = outset(op.Rect, 1)
			r.clips = append(r.clips, clip)
			clip = clip.Intersect(b)
		case scene.OpPopClip:
			if len(r.clips) > 0 {
				clip = r.clips[len(r.clips)-1]
				r.clips = r.clips[:len(r.clips)-1]
			}
		}
		out = append(out, b.Intersect(clip))
	}
	return out
}

func outset(rc scene.Rect, d float32) image.Rectangle {
	return image.Rect(int(math.Floor(float64(rc.X-d))), int(math.Floor(float64(rc.Y-d))),
		int(math.Ceil(float64(rc.X+rc.W+d))), int(math.Ceil(float64(rc.Y+rc.H+d))))
}

// remember keeps what Render needs of s to compare the next scene with.
func (r *Renderer) remember(s *scene.Scene) {
	r.valid = true
	r.clear = s.Clear
	r.ops = append(r.ops[:0], s.Ops...)
	r.glyphs = append(r.glyphs[:0], s.Glyphs...)
	r.bounds, r.next = r.next, r.bounds
	r.versions = r.versions[:0]
	for i := range s.Ops {
		v := uint64(0)
		if img := s.Ops[i].Image; img != nil {
			v = img.Version()
		}
		r.versions = append(r.versions, v)
	}
	r.mask.set(s.MaskAtlas)
	r.color.set(s.ColorAtlas)
}
