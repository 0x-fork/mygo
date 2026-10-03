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
	if e.flags&flagInvisible != 0 {
		return
	}
	if e.isInline() {
		p.paintInline(e)
		return
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
	clips := e.flags&(flagClipX|flagClipY|flagScrollX|flagScrollY) != 0
	// What an element clips meets its border where both are smoothed, on
	// rounded corners: painted over it there, it would thin the border. The
	// border goes over them instead.
	borderOver := clips && e.first != nil && scene.HasBorder(e.border) && e.borderC.A > 0
	own := p.visible(box, margin+4)
	if own {
		for _, sh := range e.shadows {
			p.shadow(box, e.radius, sh)
		}
		p.background(e, box, !borderOver)
		if e.paintFn != nil {
			e.paintFn(p, box)
		}
		switch e.kind {
		case kindText:
			ts := e.resolvedText()
			ox, oy := e.x+e.contentX(), e.y+e.contentY()
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
			c := e.resolvedText().color
			if e.gray {
				c = c.gray()
			}
			p.drawIcon(e.svg, e.contentBox(), c, e.rotate)
		case kindInput:
			e.paintInput(p)
		}
	}
	savedClip := p.clip
	if clips {
		inner, rad := e.clipRect()
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
	if own && borderOver {
		p.border(e, box)
	}
	if own && e.paintAfterFn != nil {
		e.paintAfterFn(p, box)
	}
	if e.flags&flagDebug != 0 && (e.parent == nil || e.parent.flags&flagDebug == 0) {
		p.debug(e)
	}
	if own && e.kind != kindInput && e.ringShown() {
		p.FocusRing(box, e.radius)
	}
	p.opacity = saved
}

// clipRect returns the box an element clips its children to, inside its
// border, and its radii: as far as the clip reaches along an axis it does
// not clip.
func (e *Element) clipRect() (Rect, [4]float32) {
	r := Rect{e.x + e.border[3], e.y + e.border[0], e.w - e.border[1] - e.border[3], e.h - e.border[0] - e.border[2]}
	var rad [4]float32
	all := e.flags&(flagScrollX|flagScrollY) != 0 || e.flags&flagClip == flagClip
	if all {
		for i, side := range [4][2]int{{0, 3}, {0, 1}, {2, 1}, {2, 3}} {
			rad[i] = max(e.radius[i]-max(e.border[side[0]], e.border[side[1]]), 0)
		}
		return r, rad
	}
	const far = 1e6
	if e.flags&flagClipX == 0 {
		r.X, r.W = -far, 2*far
	}
	if e.flags&flagClipY == 0 {
		r.Y, r.H = -far, 2*far
	}
	return r, rad
}

// borders returns border widths in device pixels, whole ones at least one
// wide where they are not zero.
func (p *Painter) borders(w [4]float32) [4]float32 {
	for i, v := range w {
		if v > 0 {
			w[i] = max(round(v*p.scale), 1)
		}
	}
	return w
}

// fill paints a rounded rectangle with a solid border of bw DIPs.
func (p *Painter) fill(r Rect, radius [4]float32, bg Color, bw float32, bc Color) {
	op := scene.Op{Kind: scene.OpFill, Rect: p.snap(r), Radii: p.radii(radius), Color: bg.scene(), BorderColor: bc.scene(), Opacity: p.opacity}
	if bw > 0 {
		op.Border = p.borders([4]float32{bw, bw, bw, bw})
	}
	p.s.Ops = append(p.s.Ops, op)
}

// background paints the background of an element, and its border with it
// unless withBorder is false.
func (p *Painter) background(e *Element, box Rect, withBorder bool) {
	border := withBorder && scene.HasBorder(e.border) && e.borderC.A > 0
	var visible bool
	switch e.fill {
	case fillColor:
		visible = e.bg.A > 0
	case fillGradient:
		visible = e.grad.From.A > 0 || e.grad.To.A > 0
	case fillStripes:
		visible = e.bg.A > 0 || e.stripes.c.A > 0
	}
	if !visible && !border {
		return
	}
	op := scene.Op{Kind: scene.OpFill, Rect: p.snap(box), Radii: p.radii(e.radius), Color: e.bg.scene(), Opacity: p.opacity}
	if border {
		op.Border, op.BorderColor, op.Dashed = p.borders(e.border), e.borderC.scene(), e.borderStyle == BorderDashed
	}
	switch e.fill {
	case fillGradient:
		p.gradient(&op, e.grad)
	case fillStripes:
		st := e.stripes
		a := float64(st.angle) * math.Pi / 180
		w := max(st.width*p.scale, 0.5)
		op.Paint, op.Color, op.Color2 = scene.PaintStripes, st.c.scene(), e.bg.scene()
		op.Gradient = [4]float32{float32(math.Cos(a)), float32(math.Sin(a)), w, w + max(st.gap*p.scale, 0)}
	}
	p.s.Ops = append(p.s.Ops, op)
}

// border paints e's border alone.
func (p *Painter) border(e *Element, box Rect) {
	p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpFill, Rect: p.snap(box), Radii: p.radii(e.radius), Opacity: p.opacity,
		Border: p.borders(e.border), BorderColor: e.borderC.scene(), Dashed: e.borderStyle == BorderDashed})
}

// gradient sets the paint of op to g, across its rectangle.
func (p *Painter) gradient(op *scene.Op, g LinearGradient) {
	// CSS angles: 0deg points up, 90deg right.
	a := float64(g.Angle) * math.Pi / 180
	dx, dy := float32(math.Sin(a)), float32(-math.Cos(a))
	half := (abs32(op.Rect.W*dx) + abs32(op.Rect.H*dy)) / 2
	cx, cy := op.Rect.X+op.Rect.W/2, op.Rect.Y+op.Rect.H/2
	x0, y0, x1, y1 := cx-dx*half, cy-dy*half, cx+dx*half, cy+dy*half
	start, end := g.Start, g.End
	if start == 0 && end == 0 {
		end = 1
	}
	op.Paint = scene.PaintLinear
	if g.Oklab {
		op.Paint = scene.PaintOklab
	}
	op.Color, op.Color2 = g.From.scene(), g.To.scene()
	sx, sy := x0+(x1-x0)*start, y0+(y1-y0)*start
	ex, ey := x0+(x1-x0)*end, y0+(y1-y0)*end
	if end-start < 0.5/max(2*half, 1) {
		// Stops at one place: a hard edge, half a pixel wide.
		ex, ey = sx+dx*0.5, sy+dy*0.5
	}
	op.Gradient = [4]float32{sx, sy, ex, ey}
}

// debug outlines an element and the elements inside it: their margins in
// orange, borders and padding in green, and content in blue.
func (p *Painter) debug(e *Element) {
	saved := p.opacity
	p.opacity = 1
	var walk func(e *Element)
	walk = func(e *Element) {
		if e.flags&flagInvisible != 0 {
			return
		}
		box := Rect{e.x, e.y, e.w, e.h}
		if e.marginX() > 0 || e.marginY() > 0 {
			p.fill(Rect{box.X - e.m(3), box.Y - e.m(0), box.W + e.marginX(), box.H + e.marginY()}, [4]float32{}, Color{}, 1, RGBA(249, 115, 22, 0.8))
		}
		if e.padX() > 0 || e.padY() > 0 {
			p.fill(e.contentBox(), [4]float32{}, Color{}, 1, RGBA(34, 197, 94, 0.8))
		}
		p.fill(box, [4]float32{}, Color{}, 1, RGBA(59, 130, 246, 0.9))
		for c := e.first; c != nil; c = c.next {
			walk(c)
		}
	}
	walk(e)
	p.opacity = saved
}

func (p *Painter) pushClip(r Rect, radius [4]float32) {
	p.clip = intersect(p.clip, r)
	p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpPushClip, Rect: p.snap(r), Radii: p.radii(radius)})
}

func (p *Painter) popClip() {
	p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpPopClip})
}

// textLayout paints a laid out text from (x, y), in color, with the
// background, underline or strikethrough of ts, and the colors, backgrounds
// and lines of the spans of sp, if any.
func (p *Painter) textLayout(l *text.Layout, x, y float32, color Color, ts textStyle, sp *spanPaint) {
	if l == nil {
		return
	}
	sys := p.rt.text
	s := p.scale
	deco := decoration{underline: ts.underline, wavy: ts.wavy, strike: ts.strike, color: ts.decoColor, thick: ts.decoThick}
	shade := text.ShadeOf(color.R, color.G, color.B)
	start := int32(len(p.s.Glyphs))
	for li := range l.Lines {
		line := &l.Lines[li]
		if y+line.Y > p.clip.Y+p.clip.H || y+line.Y+line.Height < p.clip.Y {
			continue
		}
		if ts.background.A > 0 && line.Width > 0 {
			p.Fill(Rect{x + line.X, y + line.Y, line.Width, line.Height}, ts.background, 0)
		}
		if sp != nil {
			sp.backgrounds(p, line, x, y)
		}
		baseline := round((y + line.Baseline) * s)
		for _, g := range line.Glyphs {
			pen := (x + g.X) * s
			if pen > (p.clip.X+p.clip.W)*s || pen+(g.Advance+g.Size)*s < p.clip.X*s {
				continue
			}
			ix := float32(math.Floor(float64(pen)))
			sub := int((pen - ix) * text.SubpixelSteps)
			glyphColor, glyphShade := color, shade
			if sp != nil {
				if glyphColor = sp.color(sp.at(g.Cluster), color); glyphColor != color {
					glyphShade = text.ShadeOf(glyphColor.R, glyphColor.G, glyphColor.B)
				}
			}
			gi := sys.Glyph(g.Font, g.ID, s, sub, glyphShade)
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
			sp.lines(p, line, x, baseline, color, deco)
		}
		if deco.underline || deco.strike {
			p.decorate((x+line.X)*s, (x+line.X+line.Width)*s, baseline, baseline-round(line.Ascent*s*0.3), ts.size, deco, color)
		}
	}
	if end := int32(len(p.s.Glyphs)); end > start {
		p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpGlyphs, Start: start, End: end})
	}
}

// decoration is how lines go through or under text.
type decoration struct {
	underline, wavy, strike bool
	// color, if set, is the lines', and thick their thickness in DIPs.
	color Color
	thick float32
}

// decorate draws the lines of d along text of size DIPs from x0 to x1, on
// a baseline at baseline, with a strikethrough at strikeY, all in device
// pixels, in the color of d or c.
func (p *Painter) decorate(x0, x1, baseline, strikeY, size float32, d decoration, c Color) {
	s := p.scale
	thick := max(round(size*s/14), 1)
	if d.thick > 0 {
		thick = max(round(d.thick*s), 1)
		// Thicker lines than the font's grow both ways.
		strikeY -= float32(math.Floor(float64(thick / 2)))
	}
	if d.color.A > 0 {
		c = d.color
	}
	x, w := round(x0), round(x1-x0)
	if w <= 0 {
		return
	}
	if d.underline {
		uy := baseline + max(round(size*s/10), 1)
		if d.wavy {
			p.wave(x, x+w, uy+thick/2, thick, c)
		} else {
			p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: x, Y: uy, W: w, H: thick}, Color: c.scene(), Opacity: p.opacity})
		}
	}
	if d.strike {
		p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: x, Y: strikeY, W: w, H: thick}, Color: c.scene(), Opacity: p.opacity})
	}
}

// wave strokes a wave from x0 to x1 around y, thick device pixels wide,
// one and a half times as high, as spell checkers underline words.
func (p *Painter) wave(x0, x1, y, thick float32, c Color) {
	s := p.scale
	amp := thick * 1.5 / 2
	length := thick * 6
	y += amp
	var path Path
	// Eight points a wave, the last at x1.
	n := max(int(math.Ceil(float64((x1-x0)/(length/8)))), 1)
	path.MoveTo(x0/s, y/s)
	for i := 1; i <= n; i++ {
		x := x0 + (x1-x0)*float32(i)/float32(n)
		path.LineTo(x/s, (y-amp*float32(math.Sin(float64((x-x0)/length*2*math.Pi))))/s)
	}
	p.StrokePath(&path, thick/s, c)
}

// contentBox returns the element's box inside its padding and border.
func (e *Element) contentBox() Rect {
	return Rect{e.x + e.contentX(), e.y + e.contentY(), e.w - e.padX(), e.h - e.padY()}
}

func (p *Painter) image(e *Element) {
	if s := e.svg; s != nil {
		p.drawSVG(s, e.contentBox(), e.fit, e.radius, e.resolvedText().color, e.gray)
		return
	}
	img := e.image
	if img == nil || img.w == 0 || img.h == 0 {
		return
	}
	p.drawBitmap(img, e.contentBox(), e.fit, e.radius, e.gray)
}

// fitIn returns where a picture w×h DIPs goes in box as fit says, and
// which part of it shows there, as fractions of its size.
func fitIn(box Rect, w, h float32, fit Fit) (dst, src Rect) {
	dst, src = box, Rect{0, 0, 1, 1}
	scale := float32(1)
	switch fit {
	case FillBox:
		return dst, src
	case Contain:
		scale = min(box.W/w, box.H/h)
	case Cover:
		scale = max(box.W/w, box.H/h)
	case ScaleDown:
		scale = min(box.W/w, box.H/h, 1)
	}
	dst.W, dst.H = w*scale, h*scale
	dst.X += (box.W - dst.W) / 2
	dst.Y += (box.H - dst.H) / 2
	// Only what falls in the box shows.
	shown := intersect(dst, box)
	src = Rect{(shown.X - dst.X) / dst.W, (shown.Y - dst.Y) / dst.H, shown.W / dst.W, shown.H / dst.H}
	return shown, src
}

func (p *Painter) drawBitmap(img *Bitmap, box Rect, fit Fit, radius [4]float32, gray bool) {
	// A bitmap's own size is its pixels in DIPs, as an Image lays it out.
	iw, ih := float32(img.w), float32(img.h)
	dst, frac := fitIn(box, iw, ih, fit)
	if dst.W <= 0 || dst.H <= 0 {
		return
	}
	src := scene.Rect{X: frac.X * iw, Y: frac.Y * ih, W: frac.W * iw, H: frac.H * ih}
	p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpImage, Rect: p.snap(dst), Radii: p.radii(radius), Image: img.img, Src: src, Opacity: p.opacity, Grayscale: gray})
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
	g := scrollBars(Rect{e.x, e.y, e.w, e.h}, e.contentW, e.contentH, st.scrollX, st.scrollY, e.flags, theme.scrollbarWidth())
	if g.vertical {
		bar := g.v
		if dragging && !rt.scrollDrag.horizontal {
			bar.X, bar.W = bar.X-2, bar.W+2
		}
		p.fill(bar, [4]float32{bar.W / 2, bar.W / 2, bar.W / 2, bar.W / 2}, color, 0, Color{})
	}
	if g.horizontal {
		bar := g.h
		if dragging && rt.scrollDrag.horizontal {
			bar.Y, bar.H = bar.Y-2, bar.H+2
		}
		p.fill(bar, [4]float32{bar.H / 2, bar.H / 2, bar.H / 2, bar.H / 2}, color, 0, Color{})
	}
}

// scrollGeometry is where the scroll bars of a container go.
type scrollGeometry struct {
	vertical, horizontal bool
	// v and h are the thumbs, vTrack and hTrack the tracks they move on.
	v, h, vTrack, hTrack Rect
}

// scrollBars returns the scroll bars of a container box scrolled by
// (x, y) over content w×h, with thumbs width DIPs wide: those of the
// directions its flags scroll that overflow, which leave each other the
// corner where both show.
func scrollBars(box Rect, w, h, x, y float32, flags uint32, width float32) scrollGeometry {
	var g scrollGeometry
	g.vertical = flags&flagScrollY != 0 && h > box.H+0.5
	g.horizontal = flags&flagScrollX != 0 && w > box.W+0.5
	corner := float32(0)
	if g.vertical && g.horizontal {
		corner = width + 3
	}
	if g.vertical {
		g.vTrack = Rect{box.X + box.W - width - 6, box.Y, width + 6, box.H - corner}
		t := scrollThumb(box.Y, box.H-corner, box.H, h, y)
		g.v = Rect{box.X + box.W - width - 3, t.Y, width, t.H}
	}
	if g.horizontal {
		g.hTrack = Rect{box.X, box.Y + box.H - width - 6, box.W - corner, width + 6}
		t := scrollThumb(box.X, box.W-corner, box.W, w, x)
		g.h = Rect{t.Y, box.Y + box.H - width - 3, t.H, width}
	}
	return g
}

// scrollThumb returns the thumb's position (Y) and length (H) along a
// track starting at pos, track long, for a view of content of size content
// scrolled by offset.
func scrollThumb(pos, track, view, content, offset float32) Rect {
	inner := track - 4
	thumb := min(max(inner*view/content, 24), inner)
	travel := inner - thumb
	at := float32(0)
	if content > view {
		at = travel * offset / (content - view)
	}
	return Rect{Y: pos + 2 + at, H: thumb}
}

// Fill paints a rounded rectangle.
func (p *Painter) Fill(r Rect, c Color, radius float32) {
	p.fill(r, [4]float32{radius, radius, radius, radius}, c, 0, Color{})
}

// FillGradient paints a rounded rectangle with a gradient.
func (p *Painter) FillGradient(r Rect, g LinearGradient, radius float32) {
	op := scene.Op{Kind: scene.OpFill, Rect: p.snap(r), Radii: p.radii([4]float32{radius, radius, radius, radius}), Opacity: p.opacity}
	p.gradient(&op, g)
	p.s.Ops = append(p.s.Ops, op)
}

// Stroke paints the outline of a rounded rectangle, width DIPs wide inside
// its edge.
func (p *Painter) Stroke(r Rect, c Color, radius, width float32) {
	p.fill(r, [4]float32{radius, radius, radius, radius}, Color{}, width, c)
}

// StrokeDashed paints the outline of a rounded rectangle in dashes, as a
// dashed border.
func (p *Painter) StrokeDashed(r Rect, c Color, radius, width float32) {
	op := scene.Op{Kind: scene.OpFill, Rect: p.snap(r), Radii: p.radii([4]float32{radius, radius, radius, radius}),
		Border: p.borders([4]float32{width, width, width, width}), BorderColor: c.scene(), Dashed: true, Opacity: p.opacity}
	p.s.Ops = append(p.s.Ops, op)
}

// Shadow paints the box shadow of a rounded rectangle as Element.Shadow
// does: offset by x and y, blurred by blur and grown by spread DIPs, and
// only outside the rectangle, so that it does not show through what is
// drawn there.
func (p *Painter) Shadow(r Rect, radius, x, y, blur, spread float32, c Color) {
	p.shadow(r, [4]float32{radius, radius, radius, radius}, shadow{x, y, blur, spread, c})
}

// shadow paints the shadow sh of box, rounded by rad, outside box, as
// CSS's box-shadow does.
func (p *Painter) shadow(box Rect, rad [4]float32, sh shadow) {
	r := Rect{box.X + sh.x - sh.spread, box.Y + sh.y - sh.spread, box.W + 2*sh.spread, box.H + 2*sh.spread}
	grown := rad
	for i := range grown {
		if grown[i] > 0 {
			grown[i] = max(grown[i]+sh.spread, 0)
		}
	}
	p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpShadow, Rect: p.snap(r), Radii: p.radii(grown), Color: sh.color.scene(),
		Blur: sh.blur * p.scale, Cast: p.snap(box), CastRadii: p.radii(rad), Opacity: p.opacity})
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
			p.drawBitmap(s, r, fit, [4]float32{}, false)
		}
	case *SVG:
		p.drawSVG(s, r, fit, [4]float32{}, p.rt.c.theme.Text, false)
	}
}

// FocusRing draws the ring that shows the keyboard focus around r.
func (p *Painter) FocusRing(r Rect, radius [4]float32) {
	const w = 2
	o := Rect{r.X - w - 1, r.Y - w - 1, r.W + 2*w + 2, r.H + 2*w + 2}
	for i := range radius {
		radius[i] += w + 1
	}
	p.fill(o, radius, Color{}, w, p.rt.c.theme.Focus)
}

// Clip restricts what fn draws to r.
func (p *Painter) Clip(r Rect, radius float32, fn func()) {
	saved := p.clip
	p.pushClip(r, [4]float32{radius, radius, radius, radius})
	fn()
	p.popClip()
	p.clip = saved
}
