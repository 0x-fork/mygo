// Package raster draws scenes in memory: the software renderer behind
// headless rendering, window captures and windows without a GPU renderer.
// Coverage comes from signed distances to rounded rectangles, so edges,
// corners and clips are anti-aliased the way the GPU shaders do it.
package raster

import (
	"image"
	"math"

	"github.com/egoist/mygo/internal/scene"
)

// Image is a premultiplied BGRA bitmap, the layout Windows DIBs, cairo
// and Core Graphics take.
type Image struct {
	W, H, Stride int
	Pix          []byte
}

// NewImage returns a w×h image.
func NewImage(w, h int) *Image {
	return &Image{W: w, H: h, Stride: 4 * w, Pix: make([]byte, 4*w*h)}
}

// Resize makes the image w×h, reusing its memory when it can.
func (m *Image) Resize(w, h int) {
	m.W, m.H, m.Stride = w, h, 4*w
	if cap(m.Pix) >= 4*w*h {
		m.Pix = m.Pix[:4*w*h]
	} else {
		m.Pix = make([]byte, 4*w*h)
	}
}

// RGBA returns the pixels as premultiplied RGBA, as image.RGBA holds them.
func (m *Image) RGBA() []byte {
	out := make([]byte, 4*m.W*m.H)
	for y := 0; y < m.H; y++ {
		src := m.Pix[y*m.Stride:]
		dst := out[y*4*m.W:]
		for x := 0; x < m.W; x++ {
			s, d := src[4*x:4*x+4], dst[4*x:4*x+4]
			d[0], d[1], d[2], d[3] = s[2], s[1], s[0], s[3]
		}
	}
	return out
}

type clip struct {
	r     scene.Rect
	radii [4]float32
	round bool
}

type renderer struct {
	dst   *Image
	s     *scene.Scene
	clips []clip
	// area is the part of dst to draw.
	area image.Rectangle
	// bounds is the intersection of the clip rectangles, in whole pixels.
	x0, y0, x1, y1 int
	// profile is scratch space for shadows.
	profile []float32
}

// Render draws s into dst, which must be s.Width×s.Height.
func Render(dst *Image, s *scene.Scene) {
	var r renderer
	r.render(dst, s, image.Rect(0, 0, dst.W, dst.H))
}

// render draws the pixels of s within area.
func (r *renderer) render(dst *Image, s *scene.Scene, area image.Rectangle) {
	r.dst, r.s, r.clips = dst, s, r.clips[:0]
	r.area = area.Intersect(image.Rect(0, 0, dst.W, dst.H))
	if r.area.Empty() {
		return
	}
	r.clear(s.Clear)
	r.updateBounds()
	for i := range s.Ops {
		op := &s.Ops[i]
		switch op.Kind {
		case scene.OpFill:
			r.fill(op)
		case scene.OpShadow:
			r.shadow(op)
		case scene.OpGlyphs:
			r.glyphs(op)
		case scene.OpImage:
			r.image(op)
		case scene.OpPushClip:
			r.clips = append(r.clips, clip{r: op.Rect, radii: fitRadii(op.Rect, op.Radii), round: hasRadii(op.Radii)})
			r.updateBounds()
		case scene.OpPopClip:
			if len(r.clips) > 0 {
				r.clips = r.clips[:len(r.clips)-1]
				r.updateBounds()
			}
		}
	}
}

func (r *renderer) clear(c scene.Color) {
	p := c.Premul(1)
	px := [4]byte{to8(p[2]), to8(p[1]), to8(p[0]), to8(p[3])}
	d, a := r.dst, r.area
	row := d.Pix[a.Min.Y*d.Stride+4*a.Min.X : a.Min.Y*d.Stride+4*a.Max.X]
	for x := 0; x < a.Dx(); x++ {
		copy(row[4*x:], px[:])
	}
	for y := a.Min.Y + 1; y < a.Max.Y; y++ {
		copy(d.Pix[y*d.Stride+4*a.Min.X:], row)
	}
}

func (r *renderer) updateBounds() {
	r.x0, r.y0, r.x1, r.y1 = r.area.Min.X, r.area.Min.Y, r.area.Max.X, r.area.Max.Y
	for _, c := range r.clips {
		r.x0 = max(r.x0, int(math.Floor(float64(c.r.X))))
		r.y0 = max(r.y0, int(math.Floor(float64(c.r.Y))))
		r.x1 = min(r.x1, int(math.Ceil(float64(c.r.X+c.r.W))))
		r.y1 = min(r.y1, int(math.Ceil(float64(c.r.Y+c.r.H))))
	}
}

// pixelBounds returns the pixels a rectangle touches within the clip.
func (r *renderer) pixelBounds(rc scene.Rect) (x0, y0, x1, y1 int) {
	x0 = max(r.x0, int(math.Floor(float64(rc.X))))
	y0 = max(r.y0, int(math.Floor(float64(rc.Y))))
	x1 = min(r.x1, int(math.Ceil(float64(rc.X+rc.W))))
	y1 = min(r.y1, int(math.Ceil(float64(rc.Y+rc.H))))
	return
}

// clipCoverage returns how much of pixel (x, y) the clips let through,
// given the clip rectangles' pixel bounds already apply.
func (r *renderer) clipCoverage(x, y int) float32 {
	cov := float32(1)
	for i := range r.clips {
		c := &r.clips[i]
		px, py := float32(x)+0.5, float32(y)+0.5
		if c.round {
			cov *= coverage(c.r, c.radii, px, py)
		} else {
			// Partial pixels at fractional clip edges.
			cov *= clamp01(min(px-c.r.X, c.r.X+c.r.W-px)+0.5) * clamp01(min(py-c.r.Y, c.r.Y+c.r.H-py)+0.5)
		}
		if cov == 0 {
			return 0
		}
	}
	return cov
}

// clipSolid returns the pixels of row y that the clips let through
// entirely, so that clipCoverage need not run for them.
func (r *renderer) clipSolid(y int) (lo, hi int) {
	lo, hi = r.x0, r.x1
	for i := range r.clips {
		c := &r.clips[i]
		l, h := solidSpan(c.r, c.radii, float32(y), float32(y+1))
		lo, hi = max(lo, l), min(hi, h)
	}
	return lo, hi
}

func hasRadii(radii [4]float32) bool {
	return radii[0] > 0 || radii[1] > 0 || radii[2] > 0 || radii[3] > 0
}

func fitRadii(rc scene.Rect, radii [4]float32) [4]float32 { return scene.FitRadii(rc, radii) }

// coverage returns how much of the pixel centered at (px, py) the rounded
// rectangle covers, from the signed distance to its edge.
func coverage(rc scene.Rect, radii [4]float32, px, py float32) float32 {
	hx, hy := rc.W/2, rc.H/2
	qx, qy := px-rc.X-hx, py-rc.Y-hy
	var rad float32
	switch {
	case qx < 0 && qy < 0:
		rad = radii[0]
	case qx >= 0 && qy < 0:
		rad = radii[1]
	case qx >= 0:
		rad = radii[2]
	default:
		rad = radii[3]
	}
	ax, ay := abs(qx)-hx+rad, abs(qy)-hy+rad
	var d float32
	if ax > 0 && ay > 0 {
		d = float32(math.Sqrt(float64(ax*ax+ay*ay))) - rad
	} else {
		d = max(ax, ay) - rad
	}
	return clamp01(0.5 - d)
}

// solidSpan returns the pixels of the row between y0 and y1 that the
// rounded rectangle covers entirely; lo >= hi when there are none.
func solidSpan(rc scene.Rect, radii [4]float32, y0, y1 float32) (lo, hi int) {
	if y0 < rc.Y || y1 > rc.Y+rc.H {
		return 0, 0
	}
	left, right := rc.X, rc.X+rc.W
	// The corners narrow the row where it is within their height.
	edge := func(rad float32, cy float32, worstY float32) float32 {
		dy := abs(cy - worstY)
		if dy >= rad {
			return rad
		}
		return rad - float32(math.Sqrt(float64(rad*rad-dy*dy)))
	}
	if rad := radii[0]; rad > 0 && y0 < rc.Y+rad {
		left = max(left, rc.X+edge(rad, rc.Y+rad, y0))
	}
	if rad := radii[3]; rad > 0 && y1 > rc.Y+rc.H-rad {
		left = max(left, rc.X+edge(rad, rc.Y+rc.H-rad, y1))
	}
	if rad := radii[1]; rad > 0 && y0 < rc.Y+rad {
		right = min(right, rc.X+rc.W-edge(rad, rc.Y+rad, y0))
	}
	if rad := radii[2]; rad > 0 && y1 > rc.Y+rc.H-rad {
		right = min(right, rc.X+rc.W-edge(rad, rc.Y+rc.H-rad, y1))
	}
	return int(math.Ceil(float64(left))), int(math.Floor(float64(right)))
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func to8(v float32) byte {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return byte(v*255 + 0.5)
}

// blend composites a premultiplied color with coverage cov over the pixel.
func blend(p []byte, c [4]float32, cov float32) {
	a := c[3] * cov
	if a <= 0 {
		return
	}
	inv := 1 - a
	p[0] = to8(c[2]*cov + float32(p[0])/255*inv)
	p[1] = to8(c[1]*cov + float32(p[1])/255*inv)
	p[2] = to8(c[0]*cov + float32(p[2])/255*inv)
	p[3] = to8(a + float32(p[3])/255*inv)
}

// blendSpan composites a premultiplied color with coverage cov over a run
// of pixels.
func blendSpan(row []byte, x0, x1 int, c [4]float32, cov float32) {
	if x1 <= x0 {
		return
	}
	a := c[3] * cov
	if a <= 0 {
		return
	}
	px := [4]byte{to8(c[2] * cov), to8(c[1] * cov), to8(c[0] * cov), to8(a)}
	run := row[4*x0 : 4*x1]
	if px[3] == 255 {
		// Opaque: copy the first pixel, then ever longer runs of them.
		copy(run, px[:])
		for n := 4; n < len(run); n *= 2 {
			copy(run[n:], run[:n])
		}
		return
	}
	inv := 255 - uint32(px[3])
	for i := 0; i < len(run); i += 4 {
		p := run[i : i+4 : i+4]
		p[0] = over(px[0], p[0], inv)
		p[1] = over(px[1], p[1], inv)
		p[2] = over(px[2], p[2], inv)
		p[3] = over(px[3], p[3], inv)
	}
}

// over returns src + dst×inv/255, rounded, for 8-bit premultiplied
// channels.
func over(src, dst byte, inv uint32) byte {
	t := uint32(dst)*inv + 128
	return byte(min(uint32(src)+(t+t>>8)>>8, 255))
}

// paint returns the op's fill color at a pixel center.
func paint(op *scene.Op, fill [4]float32, px, py float32) [4]float32 {
	if !op.HasGrad {
		return fill
	}
	g := op.Gradient
	dx, dy := g[2]-g[0], g[3]-g[1]
	t := float32(0)
	if l := dx*dx + dy*dy; l > 0 {
		t = clamp01(((px-g[0])*dx + (py-g[1])*dy) / l)
	}
	a, b := op.Color, op.Color2
	mix := func(u, v uint8) float32 { return (float32(u)*(1-t) + float32(v)*t) / 255 }
	alpha := mix(a.A, b.A) * op.Opacity
	return [4]float32{mix(a.R, b.R) * alpha, mix(a.G, b.G) * alpha, mix(a.B, b.B) * alpha, alpha}
}

func (r *renderer) fill(op *scene.Op) {
	if op.Rect.Empty() {
		return
	}
	opacity := op.Opacity
	if opacity == 0 {
		opacity = 1
	}
	outer := op.Rect
	radii := fitRadii(outer, op.Radii)
	fill := op.Color.Premul(opacity)
	border := op.BorderColor.Premul(opacity)
	bw := op.Border
	if op.BorderColor.A == 0 {
		bw = 0
	}
	inner := outer
	var innerRadii [4]float32
	if bw > 0 {
		inner = scene.Rect{X: outer.X + bw, Y: outer.Y + bw, W: outer.W - 2*bw, H: outer.H - 2*bw}
		for i, rad := range radii {
			innerRadii[i] = max(rad-bw, 0)
		}
		innerRadii = fitRadii(inner, innerRadii)
	}
	hasFill := op.Color.A > 0 || (op.HasGrad && op.Color2.A > 0)
	opGrad := *op
	opGrad.Opacity = opacity
	x0, y0, x1, y1 := r.pixelBounds(outer)
	for y := y0; y < y1; y++ {
		row := r.dst.Pix[y*r.dst.Stride:]
		py := float32(y) + 0.5
		cl, ch := r.clipSolid(y)
		ol, oh := solidSpan(outer, radii, float32(y), float32(y+1))
		il, ih := ol, oh
		if bw > 0 {
			il, ih = 0, 0
			if !inner.Empty() {
				il, ih = solidSpan(inner, innerRadii, float32(y), float32(y+1))
			}
		}
		// The middle run, inside the clips, the shape and its border, is
		// plain fill.
		sl, sh := max(il, cl, x0), min(ih, ch, x1)
		if sl < sh && hasFill && !op.HasGrad {
			blendSpan(row, sl, sh, fill, 1)
		}
		for x := x0; x < x1; x++ {
			if x >= sl && x < sh {
				if op.HasGrad {
					blend(row[4*x:4*x+4], paint(&opGrad, fill, float32(x)+0.5, py), 1)
				}
				continue
			}
			px := float32(x) + 0.5
			clipCov := float32(1)
			if x < cl || x >= ch {
				clipCov = r.clipCoverage(x, y)
				if clipCov == 0 {
					continue
				}
			}
			oc := float32(1)
			if x < ol || x >= oh {
				oc = coverage(outer, radii, px, py)
				if oc == 0 {
					continue
				}
			}
			p := row[4*x : 4*x+4]
			if hasFill {
				blend(p, paint(&opGrad, fill, px, py), oc*clipCov)
			}
			if bw > 0 {
				ic := float32(0)
				if !inner.Empty() {
					ic = coverage(inner, innerRadii, px, py)
				}
				if bc := oc - ic; bc > 0 {
					blend(p, border, bc*clipCov)
				}
			}
		}
	}
}

// shadow draws a Gaussian-blurred rounded rectangle, integrating the blur
// along y numerically and along x exactly (Evan Wallace's method).
func (r *renderer) shadow(op *scene.Op) {
	sigma := op.Blur / 2
	if sigma < 0.5 {
		f := *op
		f.Kind, f.Border, f.HasGrad = scene.OpFill, 0, false
		r.fill(&f)
		return
	}
	opacity := op.Opacity
	if opacity == 0 {
		opacity = 1
	}
	c := op.Color.Premul(opacity)
	radii := fitRadii(op.Rect, op.Radii)
	corner := max(radii[0], radii[1], radii[2], radii[3])
	ext := 3 * sigma
	box := scene.Rect{X: op.Rect.X - ext, Y: op.Rect.Y - ext, W: op.Rect.W + 2*ext, H: op.Rect.H + 2*ext}
	x0, y0, x1, y1 := r.pixelBounds(box)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	cx, cy := op.Rect.X+op.Rect.W/2, op.Rect.Y+op.Rect.H/2
	hx, hy := op.Rect.W/2, op.Rect.H/2
	k := float32(math.Sqrt(0.5)) / sigma
	// The shadow is the box blurred along y, at four samples, of a box
	// blurred exactly along x whose width the corners narrow. Where no
	// corner narrows it, a row is one horizontal profile scaled, and in the
	// middle of a row, far from the narrowed edges, the profile is 1.
	if cap(r.profile) < x1-x0 {
		r.profile = make([]float32, x1-x0)
	}
	profile := r.profile[:x1-x0]
	for x := x0; x < x1; x++ {
		px := float32(x) + 0.5 - cx
		profile[x-x0] = 0.5 * (erf((px+hx)*k) - erf((px-hx)*k))
	}
	far := 2.6 / k // where erf passes 0.9997
	for y := y0; y < y1; y++ {
		py := float32(y) + 0.5 - cy
		low, high := py-hy, py+hy
		start := min(max(-ext, low), high)
		end := min(max(ext, low), high)
		step := (end - start) / 4
		var weight, half [4]float32
		var sum float32
		narrowest := hx
		yy := start + step*0.5
		for i := range 4 {
			weight[i] = gaussian(yy, sigma) * step
			sum += weight[i]
			half[i] = hx
			if delta := min(hy-corner-abs(py-yy), 0); delta < 0 {
				half[i] = hx - corner + float32(math.Sqrt(float64(max(0, corner*corner-delta*delta))))
				narrowest = min(narrowest, half[i])
			}
			yy += step
		}
		if sum <= 0.002 {
			continue
		}
		straight := narrowest == hx
		row := r.dst.Pix[y*r.dst.Stride:]
		cl, ch := r.clipSolid(y)
		ml := max(int(math.Ceil(float64(cx-narrowest+far-0.5))), x0, cl)
		mh := min(int(math.Floor(float64(cx+narrowest-far-0.5)))+1, x1, ch)
		blendSpan(row, ml, mh, c, sum)
		for x := x0; x < x1; x++ {
			if x == ml && ml < mh {
				x = mh - 1
				continue
			}
			var v float32
			if straight {
				v = profile[x-x0] * sum
			} else {
				px := float32(x) + 0.5 - cx
				for i := range 4 {
					v += weight[i] * 0.5 * (erf((px+half[i])*k) - erf((px-half[i])*k))
				}
			}
			if v <= 0.002 {
				continue
			}
			if x < cl || x >= ch {
				v *= r.clipCoverage(x, y)
			}
			blend(row[4*x:4*x+4], c, v)
		}
	}
}

func gaussian(x, sigma float32) float32 {
	return float32(math.Exp(float64(-(x*x)/(2*sigma*sigma)))) / (float32(math.Sqrt(2*math.Pi)) * sigma)
}

// erf approximates the error function within 5e-4, as the GPU renderers
// do (Abramowitz and Stegun 7.1.27).
func erf(x float32) float32 {
	a := abs(x)
	t := 1 + (0.278393+(0.230389+0.078108*(a*a))*a)*a
	t *= t
	e := 1 - 1/(t*t)
	if x < 0 {
		return -e
	}
	return e
}

func (r *renderer) glyphs(op *scene.Op) {
	for _, g := range r.s.Glyphs[op.Start:op.End] {
		atlas := r.s.MaskAtlas
		if g.Colored {
			atlas = r.s.ColorAtlas
		}
		if atlas == nil {
			continue
		}
		gx, gy := int(math.Round(float64(g.X))), int(math.Round(float64(g.Y)))
		x0, y0 := max(gx, r.x0), max(gy, r.y0)
		x1, y1 := min(gx+int(g.UW), r.x1), min(gy+int(g.VH), r.y1)
		tint := g.Color.Premul(1)
		alpha := float32(g.Color.A) / 255
		for y := y0; y < y1; y++ {
			row := r.dst.Pix[y*r.dst.Stride:]
			cl, ch := r.clipSolid(y)
			ay := int(g.V) + y - gy
			for x := x0; x < x1; x++ {
				ax := int(g.U) + x - gx
				cov := float32(1)
				if x < cl || x >= ch {
					if cov = r.clipCoverage(x, y); cov == 0 {
						continue
					}
				}
				p := row[4*x : 4*x+4]
				if g.Colored {
					s := atlas.Pix[(ay*atlas.W+ax)*4:]
					c := [4]float32{float32(s[0]) / 255 * alpha, float32(s[1]) / 255 * alpha, float32(s[2]) / 255 * alpha, float32(s[3]) / 255 * alpha}
					blend(p, c, cov)
				} else if m := atlas.Pix[ay*atlas.W+ax]; m != 0 {
					blend(p, tint, cov*float32(m)/255)
				}
			}
		}
	}
}

func (r *renderer) image(op *scene.Op) {
	img := op.Image
	if img == nil || img.W == 0 || img.H == 0 || op.Rect.Empty() || op.Src.Empty() {
		return
	}
	opacity := op.Opacity
	if opacity == 0 {
		opacity = 1
	}
	radii := fitRadii(op.Rect, op.Radii)
	round := hasRadii(radii)
	sx, sy := op.Src.W/op.Rect.W, op.Src.H/op.Rect.H
	x0, y0, x1, y1 := r.pixelBounds(op.Rect)
	for y := y0; y < y1; y++ {
		row := r.dst.Pix[y*r.dst.Stride:]
		py := float32(y) + 0.5
		cl, ch := r.clipSolid(y)
		ol, oh := solidSpan(op.Rect, radii, float32(y), float32(y+1))
		for x := x0; x < x1; x++ {
			px := float32(x) + 0.5
			cov := opacity
			if x < ol || x >= oh || !round {
				cov *= coverage(op.Rect, radii, px, py)
			}
			if x < cl || x >= ch {
				cov *= r.clipCoverage(x, y)
			}
			if cov <= 0 {
				continue
			}
			c := sample(img, op.Src.X+(px-op.Rect.X)*sx, op.Src.Y+(py-op.Rect.Y)*sy, op.Src)
			blend(row[4*x:4*x+4], c, cov)
		}
	}
}

// sample reads a premultiplied pixel at (u, v) with bilinear filtering,
// clamped to src.
func sample(img *scene.Image, u, v float32, src scene.Rect) [4]float32 {
	u, v = u-0.5, v-0.5
	x0, y0 := int(math.Floor(float64(u))), int(math.Floor(float64(v)))
	tx, ty := u-float32(x0), v-float32(y0)
	minX, minY := int(src.X), int(src.Y)
	maxX, maxY := int(math.Ceil(float64(src.X+src.W)))-1, int(math.Ceil(float64(src.Y+src.H)))-1
	maxX, maxY = min(maxX, img.W-1), min(maxY, img.H-1)
	at := func(x, y int) []byte {
		x, y = max(minX, min(x, maxX)), max(minY, min(y, maxY))
		return img.Pix[(y*img.W+x)*4:]
	}
	p00, p10, p01, p11 := at(x0, y0), at(x0+1, y0), at(x0, y0+1), at(x0+1, y0+1)
	var c [4]float32
	for i := 0; i < 4; i++ {
		top := float32(p00[i])*(1-tx) + float32(p10[i])*tx
		bot := float32(p01[i])*(1-tx) + float32(p11[i])*tx
		c[i] = (top*(1-ty) + bot*ty) / 255
	}
	return c
}
