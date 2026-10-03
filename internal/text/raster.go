package text

import "image"

// SubpixelSteps is the number of horizontal positions within a pixel a
// glyph is rasterized for.
const SubpixelSteps = 4

type glyphKey struct {
	font  *Font
	id    uint32
	scale uint32 // pixels per DIP, in 1/256
	subX  uint8
}

// GlyphImage is a rasterized glyph in an atlas.
type GlyphImage struct {
	// OK is false for glyphs without pixels, such as spaces.
	OK bool
	// Colored glyphs are in the color atlas, the others in the mask atlas.
	Colored bool
	// X, Y, W and H are the bitmap's rectangle in its atlas.
	X, Y, W, H uint16
	// Left and Top place the bitmap relative to the glyph's origin on the
	// baseline, in pixels: Top is usually negative.
	Left, Top float32
}

// atlasEntry is a cached glyph or mask.
type atlasEntry struct {
	GlyphImage
	used uint64 // the last frame that drew it
}

// BeginFrame starts painting a frame: the masks drawn for the previous
// frame alone are forgotten.
func (s *System) BeginFrame() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.MaskAtlas.ResetTransient()
	clear(s.transient)
	s.full, s.want = [2]bool{}, [2]int{}
	s.rooms = 0
}

// Full reports whether an atlas filled up during the frame being painted
// and left out glyphs or masks it draws. MakeRoom then makes room for them.
func (s *System) Full() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.full[0] || s.full[1]
}

// MakeRoom makes room in the atlases that filled up: it keeps what the
// frame being painted drew, growing the atlas when that and what the frame
// still needs take much of it, or when room made for the frame before did
// not suffice, and forgets everything else. Everything kept moves, so the
// frame must be painted again.
func (s *System) MakeRoom() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rooms++
	for i, color := range [2]bool{false, true} {
		if s.full[i] {
			s.makeRoom(color, s.want[i], s.rooms > 1)
		}
	}
	s.full, s.want = [2]bool{}, [2]int{}
}

func (s *System) makeRoom(color bool, want int, grow bool) {
	a := s.MaskAtlas
	if color {
		a = s.ColorAtlas
	}
	var keep []*atlasEntry
	need := want
	if !color {
		for _, g := range s.transient {
			need += (int(g.W) + 1) * (int(g.H) + 1)
		}
		clear(s.transient)
		a.ResetTransient()
		for k, e := range s.masks {
			if e.used != s.frame {
				delete(s.masks, k)
				continue
			}
			keep = append(keep, e)
		}
	}
	for k, e := range s.glyphs {
		if !e.OK || e.Colored != color {
			continue // blank glyphs take no room
		}
		if e.used != s.frame {
			delete(s.glyphs, k)
			continue
		}
		keep = append(keep, e)
	}
	rects := make([]image.Rectangle, len(keep))
	for i, e := range keep {
		rects[i] = image.Rect(int(e.X), int(e.Y), int(e.X)+int(e.W), int(e.Y)+int(e.H))
		need += (int(e.W) + 1) * (int(e.H) + 1)
	}
	// Shelves waste room: keep the atlas at most half full.
	if grow {
		a.Grow()
	}
	for need*2 > a.W*a.H && a.Grow() {
	}
	pos, ok := a.Repack(rects)
	if !ok {
		a.Reset()
		for k, e := range s.glyphs {
			if e.OK && e.Colored == color {
				delete(s.glyphs, k)
			}
		}
		if !color {
			clear(s.masks)
		}
		return
	}
	for i, e := range keep {
		e.X, e.Y = uint16(pos[i].X), uint16(pos[i].Y)
	}
}

// Glyph rasterizes glyph id of a font at scale pixels per DIP, shifted
// right by subX/SubpixelSteps of a pixel.
func (s *System) Glyph(f *Font, id uint32, scale float32, subX int) GlyphImage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f == nil {
		return GlyphImage{}
	}
	key := glyphKey{font: f, id: id, scale: uint32(scale*256 + 0.5), subX: uint8(subX)}
	if e, ok := s.glyphs[key]; ok {
		e.used = s.frame
		return e.GlyphImage
	}
	failed := s.failed
	g := s.rasterize(f, id, scale, float32(subX)/SubpixelSteps)
	if s.failed == failed { // not left out of a full atlas
		s.glyphs[key] = &atlasEntry{g, s.frame}
	}
	return g
}

func (s *System) rasterize(f *Font, id uint32, scale, dx float32) GlyphImage {
	b := s.engine().glyph(f, id, scale, dx)
	if b.w <= 0 || b.h <= 0 || b.w > 2048 || b.h > 2048 {
		return GlyphImage{}
	}
	x, y, ok := s.alloc(b.color, b.w, b.h)
	if !ok {
		return GlyphImage{}
	}
	if b.color {
		s.ColorAtlas.Put(x, y, b.w, b.h, b.pix, 4*b.w)
	} else {
		s.MaskAtlas.Put(x, y, b.w, b.h, b.pix, b.w)
	}
	return GlyphImage{OK: true, Colored: b.color, X: uint16(x), Y: uint16(y), W: uint16(b.w), H: uint16(b.h), Left: float32(b.left), Top: float32(b.top)}
}

// alloc finds lasting room in an atlas, or records that it is full.
func (s *System) alloc(color bool, w, h int) (int, int, bool) {
	a := s.MaskAtlas
	if color {
		a = s.ColorAtlas
	}
	if x, y, ok := a.Alloc(w, h); ok {
		return x, y, true
	}
	s.leftOut(color, w, h)
	return 0, 0, false
}

func (s *System) leftOut(color bool, w, h int) {
	i := 0
	if color {
		i = 1
	}
	s.full[i] = true
	s.want[i] += (w + 1) * (h + 1)
	s.failed++
}

// Mask returns a coverage mask cached under key in the mask atlas,
// drawing it when it is not: draw returns its size and one byte per pixel.
// Icons and other vector shapes use it.
//
// A mask a frame draws for the first time may be one frame of an
// animation, never drawn again: it lasts for that frame only, in the
// atlas's transient room, and is cached once a later frame draws it too.
func (s *System) Mask(key uint64, draw func() (w, h int, pix []byte)) GlyphImage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.masks[key]; ok {
		e.used = s.frame
		return e.GlyphImage
	}
	if g, ok := s.transient[key]; ok {
		return g
	}
	last, seen := s.recent[key]
	lasting := seen && last != s.frame
	w, h, pix := draw()
	if w <= 0 || h <= 0 {
		return GlyphImage{}
	}
	var x, y int
	var ok bool
	if lasting {
		x, y, ok = s.MaskAtlas.Alloc(w, h)
	} else {
		x, y, ok = s.MaskAtlas.AllocTransient(w, h)
	}
	if !ok {
		s.leftOut(false, w, h)
		return GlyphImage{}
	}
	s.MaskAtlas.Put(x, y, w, h, pix, w)
	g := GlyphImage{OK: true, X: uint16(x), Y: uint16(y), W: uint16(w), H: uint16(h)}
	if lasting {
		delete(s.recent, key)
		s.masks[key] = &atlasEntry{g, s.frame}
	} else {
		s.transient[key] = g
		s.recent[key] = s.frame
	}
	return g
}
