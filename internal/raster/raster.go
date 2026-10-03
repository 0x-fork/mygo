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
	if rad <= 0 {
		// A square corner: the area of the pixel inside the box, exact
		// for lines thinner than a pixel too.
		cx := min(rc.X+rc.W, px+0.5) - max(rc.X, px-0.5)
		cy := min(rc.Y+rc.H, py+0.5) - max(rc.Y, py-0.5)
		return clamp01(cx) * clamp01(cy)
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

// blendSubpixel composites a straight color c over a pixel with the
// coverage a of each subpixel, times w, each channel by its own: as
// renderers blend subpixel glyphs with a second color for the source's
// alpha.
func blendSubpixel(p []byte, c, a [3]float32, w float32) {
	wr, wg, wb := a[0]*w, a[1]*w, a[2]*w
	wa := (wr + wg + wb) / 3
	p[0] = to8(c[2]*wb + float32(p[0])/255*(1-wb))
	p[1] = to8(c[1]*wg + float32(p[1])/255*(1-wg))
	p[2] = to8(c[0]*wr + float32(p[2])/255*(1-wr))
	p[3] = to8(wa + float32(p[3])/255*(1-wa))
}

// unpremul returns the straight color of a premultiplied one.
func unpremul(c [4]float32) [3]float32 {
	if c[3] <= 0 {
		return [3]float32{}
	}
	return [3]float32{c[0] / c[3], c[1] / c[3], c[2] / c[3]}
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

// painter computes the fill of an op at pixel centers: its color, or its
// gradient or stripes, premultiplied and times the op's opacity.
type painter struct {
	paint      scene.Paint
	c1, c2     [4]float32 // premultiplied
	lab1, lab2 [3]float32 // premultiplied Oklab
	g          [4]float32
	origin     [2]float32
}

func newPainter(op *scene.Op, opacity float32) painter {
	p := painter{paint: op.Paint, c1: op.Color.Premul(opacity), c2: op.Color2.Premul(opacity), g: op.Gradient, origin: [2]float32{op.Rect.X, op.Rect.Y}}
	if p.paint == scene.PaintOklab {
		p.lab1, p.lab2 = oklab(op.Color), oklab(op.Color2)
		for i := range 3 {
			p.lab1[i] *= p.c1[3]
			p.lab2[i] *= p.c2[3]
		}
	}
	return p
}

// solid reports whether the fill is one color.
func (p *painter) solid() bool { return p.paint == scene.PaintSolid }

// visible reports whether the fill shows anywhere.
func (p *painter) visible() bool { return p.c1[3] > 0 || (!p.solid() && p.c2[3] > 0) }

// at returns the fill at a pixel center.
func (p *painter) at(px, py float32) [4]float32 {
	switch p.paint {
	case scene.PaintSolid:
		return p.c1
	case scene.PaintStripes:
		s := (px-p.origin[0])*p.g[0] + (py-p.origin[1])*p.g[1]
		period := p.g[3]
		phase := s - period*float32(math.Floor(float64(s/period)))
		cov := clamp01(0.5 - min(max(-phase, phase-p.g[2]), period-phase))
		var c [4]float32
		for i := range c {
			c[i] = p.c1[i]*cov + p.c2[i]*(1-cov)
		}
		return c
	}
	g := p.g
	dx, dy := g[2]-g[0], g[3]-g[1]
	t := float32(0)
	if l := dx*dx + dy*dy; l > 0 {
		t = clamp01(((px-g[0])*dx + (py-g[1])*dy) / max(l, 0.0001))
	}
	a := p.c1[3]*(1-t) + p.c2[3]*t
	if p.paint == scene.PaintLinear {
		return [4]float32{p.c1[0]*(1-t) + p.c2[0]*t, p.c1[1]*(1-t) + p.c2[1]*t, p.c1[2]*(1-t) + p.c2[2]*t, a}
	}
	if a <= 0 {
		return [4]float32{}
	}
	var lab [3]float32
	for i := range lab {
		lab[i] = (p.lab1[i]*(1-t) + p.lab2[i]*t) / a
	}
	rgb := fromOklab(lab)
	return [4]float32{clamp01(rgb[0]) * a, clamp01(rgb[1]) * a, clamp01(rgb[2]) * a, a}
}

func toLinear(c float32) float32 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return float32(math.Pow(float64((c+0.055)/1.055), 2.4))
}

func toSRGB(c float32) float32 {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return 1.055*float32(math.Pow(float64(max(c, 0)), 1/2.4)) - 0.055
}

// oklab converts an sRGB color to Oklab, as the shaders do.
func oklab(c scene.Color) [3]float32 {
	r, g, b := toLinear(float32(c.R)/255), toLinear(float32(c.G)/255), toLinear(float32(c.B)/255)
	l := float32(math.Cbrt(float64(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)))
	m := float32(math.Cbrt(float64(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)))
	s := float32(math.Cbrt(float64(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)))
	return [3]float32{
		0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

// fromOklab converts an Oklab color to sRGB components, unclamped.
func fromOklab(lab [3]float32) [3]float32 {
	l := lab[0] + 0.3963377774*lab[1] + 0.2158037573*lab[2]
	m := lab[0] - 0.1055613458*lab[1] - 0.0638541728*lab[2]
	s := lab[0] - 0.0894841775*lab[1] - 1.2914855480*lab[2]
	l, m, s = l*l*l, m*m*m, s*s*s
	return [3]float32{
		toSRGB(4.0767416621*l - 3.3077115913*m + 0.2309699292*s),
		toSRGB(-1.2684380046*l + 2.6097574011*m - 0.3413193965*s),
		toSRGB(-0.0041960863*l - 0.7034186147*m + 1.7076147010*s),
	}
}

// dash returns how much of a dashed border of widths w (top, right,
// bottom, left) around r shows at a pixel center, as the shaders compute
// it: each side, which the pixel belongs to when it is nearest that side's
// edge in widths of its border, has an odd number of dashes and gaps of
// equal length, about three widths, starting and ending with a dash.
func dash(r scene.Rect, w [4]float32, px, py float32) float32 {
	qx, qy := px-r.X, py-r.Y
	d := func(dist, width float32) float32 {
		if width > 0 {
			return dist / width
		}
		return 1e9
	}
	dt, dr, db, dl := d(qy, w[0]), d(r.W-qx, w[1]), d(r.H-qy, w[2]), d(qx, w[3])
	var s, length, bw float32
	switch {
	case dt <= dr && dt <= db && dt <= dl:
		s, length, bw = qx, r.W, w[0]
	case dr <= db && dr <= dl:
		s, length, bw = qy, r.H, w[1]
	case db <= dl:
		s, length, bw = r.W-qx, r.W, w[2]
	default:
		s, length, bw = r.H-qy, r.H, w[3]
	}
	n := max(1, float32(math.Floor(float64((length/(3*bw)+1)*0.5+0.5))))
	seg := length / (2*n - 1)
	k := float32(math.Floor(float64(s / seg)))
	f := s - k*seg
	edge := min(f, seg-f)
	if k-2*float32(math.Floor(float64(k*0.5))) < 0.5 {
		edge = -edge
	}
	return clamp01(0.5 - edge)
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
	pt := newPainter(op, opacity)
	border := op.BorderColor.Premul(opacity)
	bw := op.Border
	if op.BorderColor.A == 0 {
		bw = [4]float32{}
	}
	hasBorder := scene.HasBorder(bw)
	inner := outer
	var innerRadii [4]float32
	if hasBorder {
		inner, innerRadii = scene.InnerRadii(outer, radii, bw)
	}
	hasFill, solid := pt.visible(), pt.solid()
	x0, y0, x1, y1 := r.pixelBounds(outer)
	for y := y0; y < y1; y++ {
		row := r.dst.Pix[y*r.dst.Stride:]
		py := float32(y) + 0.5
		cl, ch := r.clipSolid(y)
		ol, oh := solidSpan(outer, radii, float32(y), float32(y+1))
		il, ih := ol, oh
		if hasBorder {
			il, ih = 0, 0
			if !inner.Empty() {
				il, ih = solidSpan(inner, innerRadii, float32(y), float32(y+1))
			}
		}
		// The middle run, inside the clips, the shape and its border, is
		// plain fill.
		sl, sh := max(il, cl, x0), min(ih, ch, x1)
		if sl < sh && hasFill && solid {
			blendSpan(row, sl, sh, pt.c1, 1)
		}
		for x := x0; x < x1; x++ {
			if x >= sl && x < sh {
				if !hasFill || solid {
					x = sh - 1 // past the run, which is done
					continue
				}
				blend(row[4*x:4*x+4], pt.at(float32(x)+0.5, py), 1)
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
				blend(p, pt.at(px, py), oc*clipCov)
			}
			if hasBorder {
				ic := float32(0)
				if !inner.Empty() {
					ic = coverage(inner, innerRadii, px, py)
				}
				bc := oc - ic
				if bc > 0 && op.Dashed {
					bc *= dash(outer, bw, px, py)
				}
				if bc > 0 {
					blend(p, border, bc*clipCov)
				}
			}
		}
	}
}

// shadow draws a Gaussian-blurred rounded rectangle, integrating the blur
// along y numerically and along x exactly (Evan Wallace's method), outside
// the box casting it, if any.
func (r *renderer) shadow(op *scene.Op) {
	sigma := op.Blur / 2
	cast := !op.Cast.Empty()
	if sigma < 0.5 && !cast {
		f := *op
		f.Kind, f.Border, f.Paint = scene.OpFill, [4]float32{}, scene.PaintSolid
		r.fill(&f)
		return
	}
	opacity := op.Opacity
	if opacity == 0 {
		opacity = 1
	}
	c := op.Color.Premul(opacity)
	radii := fitRadii(op.Rect, op.Radii)
	castRadii := fitRadii(op.Cast, op.CastRadii)
	// outside returns how much of pixel (x, y) the box casting the shadow
	// leaves to it.
	outside := func(x, y int) float32 {
		return 1 - coverage(op.Cast, castRadii, float32(x)+0.5, float32(y)+0.5)
	}
	if sigma < 0.5 {
		// The box itself, outside the box casting it.
		x0, y0, x1, y1 := r.pixelBounds(op.Rect)
		for y := y0; y < y1; y++ {
			row := r.dst.Pix[y*r.dst.Stride:]
			for x := x0; x < x1; x++ {
				v := coverage(op.Rect, radii, float32(x)+0.5, float32(y)+0.5) * outside(x, y)
				if v > 0 {
					blend(row[4*x:4*x+4], c, v*r.clipCoverage(x, y))
				}
			}
		}
		return
	}
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
	// kx0..kx1 and ky0..ky1 are the pixels the box casting the shadow
	// touches.
	var kx0, ky0, kx1, ky1 int
	if cast {
		kx0, ky0 = int(math.Floor(float64(op.Cast.X))), int(math.Floor(float64(op.Cast.Y)))
		kx1, ky1 = int(math.Ceil(float64(op.Cast.X+op.Cast.W))), int(math.Ceil(float64(op.Cast.Y+op.Cast.H)))
	}
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
		// On the row, the box casting the shadow touches kl..kh, which the
		// middle leaves out, and hides it in sl..sh.
		var kl, kh, sl, sh int
		if cast && y >= ky0 && y < ky1 {
			kl, kh = kx0, kx1
			sl, sh = solidSpan(op.Cast, castRadii, float32(y), float32(y+1))
		}
		if kl < kh {
			blendSpan(row, ml, min(mh, kl), c, sum)
			blendSpan(row, max(ml, kh), mh, c, sum)
		} else {
			blendSpan(row, ml, mh, c, sum)
		}
		for x := x0; x < x1; x++ {
			if x >= ml && x < mh && (x < kl || x >= kh) {
				x = mh - 1
				if x < kl {
					x = min(mh, kl) - 1
				}
				continue
			}
			if x >= sl && x < sh {
				x = sh - 1
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
			if x >= kl && x < kh {
				v *= outside(x, y)
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
	var pt *painter
	if op.Paint == scene.PaintLinear || op.Paint == scene.PaintOklab {
		opacity := op.Opacity
		if opacity == 0 {
			opacity = 1
		}
		p := newPainter(op, opacity)
		pt = &p
	}
	text := r.s.Text
	for _, g := range r.s.Glyphs[op.Start:op.End] {
		atlas := r.s.MaskAtlas
		if g.Colored || g.Subpixel {
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
		contrast, boost := text.Contrast, float32(0)
		if g.Subpixel {
			contrast = text.SubpixelContrast
		}
		if g.Thin {
			boost = scene.ThinBoost
		}
		correct := contrast != 0 || boost != 0 || text.GammaRatios != [4]float32{}
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
				switch {
				case g.Colored:
					s := atlas.Pix[(ay*atlas.W+ax)*4:]
					c := [4]float32{float32(s[0]) / 255 * alpha, float32(s[1]) / 255 * alpha, float32(s[2]) / 255 * alpha, float32(s[3]) / 255 * alpha}
					blend(p, c, cov)
				case g.Subpixel:
					s := atlas.Pix[(ay*atlas.W+ax)*4:]
					if s[0]|s[1]|s[2] == 0 {
						continue
					}
					if pt != nil {
						tint = pt.at(float32(x)+0.5, float32(y)+0.5)
					}
					c := unpremul(tint)
					a := [3]float32{float32(s[0]) / 255, float32(s[1]) / 255, float32(s[2]) / 255}
					if correct {
						a = scene.SubpixelCoverage(a, c, contrast, boost, text.GammaRatios)
					}
					blendSubpixel(p, c, a, tint[3]*cov)
				default:
					m := atlas.Pix[ay*atlas.W+ax]
					if m == 0 {
						continue
					}
					if pt != nil {
						tint = pt.at(float32(x)+0.5, float32(y)+0.5)
					}
					a := float32(m) / 255
					if correct {
						a = scene.TextCoverage(a, unpremul(tint), contrast, boost, text.GammaRatios)
					}
					blend(p, tint, cov*a)
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
			if op.Grayscale {
				l := 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
				c[0], c[1], c[2] = l, l, l
			}
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
