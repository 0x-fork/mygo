package ui

import (
	"math"

	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/text"
)

// Painter draws an element's own content in Element.Draw and DrawOver
// callbacks, in DIPs relative to the window.
type Painter struct {
	rt      *engine
	s       *scene.Scene
	scale   float32
	opacity float32
	clip    Rect
}

func (rt *engine) paint(root *Element, w, h, scale float32) {
	s := &rt.scene
	// The root paints the theme's background: frames start transparent, so
	// that a transparent root shows what is behind the content, such as a
	// window's vibrancy.
	s.Reset(int(math.Ceil(float64(w*scale))), int(math.Ceil(float64(h*scale))), scene.Color{})
	s.Scale = scale
	s.MaskAtlas, s.ColorAtlas = rt.text.MaskAtlas, rt.text.ColorAtlas
	p := &Painter{rt: rt, s: s, scale: scale, opacity: 1, clip: Rect{0, 0, w, h}}
	p.element(root)
}

// snap converts a rectangle to device pixels, rounding its edges to whole
// pixels so that edges stay crisp.
func (p *Painter) snap(r Rect) scene.Rect {
	s := p.scale
	x0, y0 := round(r.X*s), round(r.Y*s)
	x1, y1 := round((r.X+r.W)*s), round((r.Y+r.H)*s)
	return scene.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

func round(v float32) float32 { return float32(math.Round(float64(v))) }

func (p *Painter) radii(r [4]float32) [4]float32 {
	return [4]float32{r[0] * p.scale, r[1] * p.scale, r[2] * p.scale, r[3] * p.scale}
}

func (p *Painter) visible(r Rect, margin float32) bool {
	return r.X-margin < p.clip.X+p.clip.W && r.Y-margin < p.clip.Y+p.clip.H &&
		r.X+r.W+margin > p.clip.X && r.Y+r.H+margin > p.clip.Y
}

func (p *Painter) element(e *Element) {
	if e.styleFn != nil {
		e.styleFn(e)
	}
	saved := p.opacity
	if e.flags&flagDisabled != 0 {
		p.opacity *= 0.5
	}
	if e.opacitySet {
		p.opacity *= e.opacity
	}
	if p.opacity <= 0.001 {
		p.opacity = saved
		return
	}
	box := Rect{e.x, e.y, e.w, e.h}
	margin := float32(0)
	for _, sh := range e.shadows {
		margin = max(margin, abs32(sh.x)+abs32(sh.y)+sh.blur+sh.spread)
	}
	clips := e.flags&(flagClip|flagScrollX|flagScrollY) != 0
	own := p.visible(box, margin+4)
	if own {
		for _, sh := range e.shadows {
			r := Rect{box.X + sh.x - sh.spread, box.Y + sh.y - sh.spread, box.W + 2*sh.spread, box.H + 2*sh.spread}
			rad := e.radius
			for i := range rad {
				if rad[i] > 0 {
					rad[i] = max(rad[i]+sh.spread, 0)
				}
			}
			p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpShadow, Rect: p.snap(r), Radii: p.radii(rad), Color: sh.color.scene(), Blur: sh.blur * p.scale, Opacity: p.opacity})
		}
		if e.bg.A > 0 || (e.hasGrad && e.bg2.A > 0) || (e.borderW > 0 && e.borderC.A > 0) {
			p.fill(box, e.radius, e.bg, e.borderW, e.borderC, e)
		}
		if e.paintFn != nil {
			e.paintFn(p, box)
		}
		switch e.kind {
		case kindText:
			ts := e.resolvedText()
			ox, oy := e.x+e.pad[3]+e.borderW, e.y+e.pad[0]+e.borderW
			if ed := e.st.editor; ed != nil && e.flags&flagSelectable != 0 && e.Focused() {
				if a, b := ed.selection(); a != b {
					for _, r := range e.tl.Selection(a, b) {
						p.Fill(Rect{ox + r.X, oy + r.Y, r.W, r.H}, e.c.theme.Selection, 0)
					}
				}
			}
			p.textLayout(e.tl, ox, oy, ts.color, ts, newSpanPaint(e.spans))
		case kindImage:
			p.image(e)
		case kindIcon:
			p.drawIcon(e.svg, e.contentBox(), e.resolvedText().color)
		case kindInput:
			e.paintInput(p)
		}
	}
	savedClip := p.clip
	if clips {
		inner := Rect{box.X + e.borderW, box.Y + e.borderW, box.W - 2*e.borderW, box.H - 2*e.borderW}
		rad := e.radius
		for i := range rad {
			rad[i] = max(rad[i]-e.borderW, 0)
		}
		p.pushClip(inner, rad)
	}
	if !clips || p.clip.W > 0 && p.clip.H > 0 {
		for c := e.first; c != nil; c = c.next {
			if c.flags&flagAbsolute == 0 {
				p.element(c)
			}
		}
		for c := e.first; c != nil; c = c.next {
			if c.flags&flagAbsolute != 0 {
				p.element(c)
			}
		}
	}
	if clips {
		p.popClip()
		p.clip = savedClip
	}
	if e.scrolls() && own {
		p.scrollbars(e)
	}
	if own && e.paintAfterFn != nil {
		e.paintAfterFn(p, box)
	}
	if own && e.flags&(flagFocusable|flagOwnRing) == flagFocusable && e.kind != kindInput && e.c.rt.focused == e.id && e.c.rt.focusVisible && e.c.rt.windowFocused {
		p.FocusRing(box, e.radius)
	}
	p.opacity = saved
}

func (p *Painter) fill(r Rect, radius [4]float32, bg Color, bw float32, bc Color, e *Element) {
	op := scene.Op{Kind: scene.OpFill, Rect: p.snap(r), Radii: p.radii(radius), Color: bg.scene(), Border: bw * p.scale, BorderColor: bc.scene(), Opacity: p.opacity}
	if bw > 0 {
		op.Border = max(round(bw*p.scale), 1)
	}
	if e != nil && e.hasGrad {
		// CSS angles: 0deg points up, 90deg right.
		a := float64(e.gradAngle) * math.Pi / 180
		dx, dy := float32(math.Sin(a)), float32(-math.Cos(a))
		half := (abs32(op.Rect.W*dx) + abs32(op.Rect.H*dy)) / 2
		cx, cy := op.Rect.X+op.Rect.W/2, op.Rect.Y+op.Rect.H/2
		op.HasGrad = true
		op.Color2 = e.bg2.scene()
		op.Gradient = [4]float32{cx - dx*half, cy - dy*half, cx + dx*half, cy + dy*half}
	}
	p.s.Ops = append(p.s.Ops, op)
}

func (p *Painter) pushClip(r Rect, radius [4]float32) {
	p.clip = intersect(p.clip, r)
	p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpPushClip, Rect: p.snap(r), Radii: p.radii(radius)})
}

func (p *Painter) popClip() {
	p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpPopClip})
}

// textLayout draws a text layout with its top-left corner at (x, y).
// textLayout paints a laid out text from (x, y), in color, with the
// underline or strikethrough of ts, and the colors and lines of the spans
// of sp, if any.
func (p *Painter) textLayout(l *text.Layout, x, y float32, color Color, ts textStyle, sp *spanPaint) {
	if l == nil {
		return
	}
	sys := p.rt.text
	s := p.scale
	start := int32(len(p.s.Glyphs))
	for li := range l.Lines {
		line := &l.Lines[li]
		if y+line.Y > p.clip.Y+p.clip.H || y+line.Y+line.Height < p.clip.Y {
			continue
		}
		baseline := round((y + line.Baseline) * s)
		for _, g := range line.Glyphs {
			pen := (x + g.X) * s
			if pen > (p.clip.X+p.clip.W)*s || pen+(g.Advance+g.Size)*s < p.clip.X*s {
				continue
			}
			ix := float32(math.Floor(float64(pen)))
			sub := int((pen - ix) * text.SubpixelSteps)
			gi := sys.Glyph(g.Font, g.ID, s, sub)
			glyphColor := color
			if sp != nil {
				glyphColor = sp.color(sp.at(g.Cluster), color)
			}
			if !gi.OK {
				continue
			}
			p.s.Glyphs = append(p.s.Glyphs, scene.Glyph{
				X: ix + gi.Left, Y: baseline + gi.Top, W: float32(gi.W), H: float32(gi.H),
				U: gi.X, V: gi.Y, UW: gi.W, VH: gi.H,
				Color: glyphColor.Alpha(p.opacity).scene(), Colored: gi.Colored,
			})
		}
		if sp != nil {
			sp.lines(p, line, x, baseline, color)
		}
		if ts.underline || ts.strike {
			thick := max(round(ts.size*s/14), 1)
			ly := baseline + max(round(ts.size*s/10), 1)
			if ts.strike {
				ly = baseline - round(line.Ascent*s*0.3)
			}
			p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: round((x + line.X) * s), Y: ly, W: round(line.Width * s), H: thick}, Color: color.scene(), Opacity: p.opacity})
		}
	}
	if end := int32(len(p.s.Glyphs)); end > start {
		p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpGlyphs, Start: start, End: end})
	}
}

// contentBox returns the element's box inside its padding and border.
func (e *Element) contentBox() Rect {
	return Rect{e.x + e.pad[3] + e.borderW, e.y + e.pad[0] + e.borderW, e.w - e.padX(), e.h - e.padY()}
}

func (p *Painter) image(e *Element) {
	if s := e.svg; s != nil {
		p.drawSVG(s, e.contentBox(), e.fit, e.radius, e.resolvedText().color)
		return
	}
	img := e.image
	if img == nil || img.w == 0 || img.h == 0 {
		return
	}
	p.drawBitmap(img, e.contentBox(), e.fit, e.radius)
}

func (p *Painter) drawBitmap(img *Bitmap, box Rect, fit Fit, radius [4]float32) {
	src := scene.Rect{W: float32(img.w), H: float32(img.h)}
	dst := box
	iw, ih := float32(img.w), float32(img.h)
	switch fit {
	case Contain:
		f := min(box.W/iw, box.H/ih)
		dst.W, dst.H = iw*f, ih*f
		dst.X += (box.W - dst.W) / 2
		dst.Y += (box.H - dst.H) / 2
	case Cover:
		f := max(box.W/iw, box.H/ih)
		sw, sh := box.W/f, box.H/f
		src = scene.Rect{X: (iw - sw) / 2, Y: (ih - sh) / 2, W: sw, H: sh}
	}
	p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpImage, Rect: p.snap(dst), Radii: p.radii(radius), Image: img.img, Src: src, Opacity: p.opacity})
}

// scrollbars draws the thumbs of a scroll container whose content
// overflows it.
func (p *Painter) scrollbars(e *Element) {
	st := e.st
	rt := e.c.rt
	theme := e.c.theme
	hovered := false
	for _, id := range rt.hover {
		if id == e.id {
			hovered = true
			break
		}
	}
	dragging := rt.scrollDrag.st == st
	if !hovered && !dragging {
		return
	}
	color := theme.Scrollbar
	if e.flags&flagScrollY != 0 && e.contentH > e.h+0.5 {
		r := scrollThumb(e.y, e.h, e.contentH, st.scrollY)
		bar := Rect{e.x + e.w - 9, r.Y, 6, r.H}
		if dragging {
			bar.X, bar.W = e.x+e.w-11, 8
		}
		p.fill(bar, [4]float32{bar.W / 2, bar.W / 2, bar.W / 2, bar.W / 2}, color, 0, Color{}, nil)
	}
	if e.flags&flagScrollX != 0 && e.contentW > e.w+0.5 {
		r := scrollThumb(e.x, e.w, e.contentW, st.scrollX)
		bar := Rect{r.Y, e.y + e.h - 9, r.H, 6}
		p.fill(bar, [4]float32{3, 3, 3, 3}, color, 0, Color{}, nil)
	}
}

// scrollThumb returns the thumb's position (Y) and length (H) along a
// track starting at pos, len long, for content of size content scrolled by
// offset.
func scrollThumb(pos, length, content, offset float32) Rect {
	track := length - 4
	thumb := max(track*length/content, 24)
	travel := track - thumb
	at := float32(0)
	if content > length {
		at = travel * offset / (content - length)
	}
	return Rect{Y: pos + 2 + at, H: thumb}
}

// Fill paints a rounded rectangle.
func (p *Painter) Fill(r Rect, c Color, radius float32) {
	p.fill(r, [4]float32{radius, radius, radius, radius}, c, 0, Color{}, nil)
}

// Stroke paints the outline of a rounded rectangle, width DIPs wide inside
// its edge.
func (p *Painter) Stroke(r Rect, c Color, radius, width float32) {
	p.fill(r, [4]float32{radius, radius, radius, radius}, Color{}, width, c, nil)
}

// Shadow paints a box shadow under a rounded rectangle.
func (p *Painter) Shadow(r Rect, radius, blur float32, c Color) {
	p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpShadow, Rect: p.snap(r), Radii: p.radii([4]float32{radius, radius, radius, radius}), Color: c.scene(), Blur: blur * p.scale, Opacity: p.opacity})
}

// Line paints a straight horizontal or vertical line between two points,
// width DIPs thick.
func (p *Painter) Line(x0, y0, x1, y1, width float32, c Color) {
	r := Rect{min(x0, x1), min(y0, y1), abs32(x1 - x0), abs32(y1 - y0)}
	if r.W < r.H {
		r.X -= width / 2
		r.W = width
	} else {
		r.Y -= width / 2
		r.H = width
	}
	p.Fill(r, c, 0)
}

// Text draws a line of text with its top-left corner at (x, y).
func (p *Painter) Text(x, y float32, s string, size float32, c Color) {
	l := p.rt.text.Layout(text.Params{Text: s, Style: text.Style{Size: size}})
	p.textLayout(l, x, y, c, textStyle{size: size}, nil)
}

// Image draws a bitmap, or an SVG in its own colors (with the theme's text
// color for its currentColor), scaled to fit r.
func (p *Painter) Image(src ImageSource, r Rect, fit Fit) {
	switch s := src.(type) {
	case *Bitmap:
		if s != nil && s.w > 0 && s.h > 0 {
			p.drawBitmap(s, r, fit, [4]float32{})
		}
	case *SVG:
		p.drawSVG(s, r, fit, [4]float32{}, p.rt.c.theme.Text)
	}
}

// FocusRing draws the ring that shows the keyboard focus around r.
func (p *Painter) FocusRing(r Rect, radius [4]float32) {
	const w = 2
	o := Rect{r.X - w - 1, r.Y - w - 1, r.W + 2*w + 2, r.H + 2*w + 2}
	for i := range radius {
		radius[i] += w + 1
	}
	p.fill(o, radius, Color{}, w, p.rt.c.theme.Focus, nil)
}

// Clip restricts what fn draws to r.
func (p *Painter) Clip(r Rect, radius float32, fn func()) {
	saved := p.clip
	p.pushClip(r, [4]float32{radius, radius, radius, radius})
	fn()
	p.popClip()
	p.clip = saved
}
