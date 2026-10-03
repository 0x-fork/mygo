package ui

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // DecodeBitmap
	_ "image/jpeg" // DecodeBitmap
	_ "image/png"  // DecodeBitmap
	"math"
	"time"

	"github.com/egoist/mygo/internal/scene"
)

// Box creates a container that lays its children out in a column.
func Box(c *Context) *Element { return c.newElement(kindBox) }

// Column creates a container that lays its children out from top to
// bottom, stretched to its width.
func Column(c *Context) *Element { return c.newElement(kindBox) }

// Row creates a container that lays its children out from left to right,
// centered vertically.
func Row(c *Context) *Element { return c.newElement(kindBox).Row() }

// Text creates a text, which wraps at the width it gets.
func Text(c *Context, s string) *Element {
	e := c.newElement(kindText)
	e.text = s
	return e
}

// Textf creates a text formatted with fmt.Sprintf.
func Textf(c *Context, format string, args ...any) *Element {
	return Text(c, fmt.Sprintf(format, args...))
}

// Spacer creates an empty element that takes the free space of its row or
// column, pushing its siblings apart.
func Spacer(c *Context) *Element { return Box(c).Grow(1) }

// Divider creates a thin line across its row or column.
func Divider(c *Context) *Element {
	row := c.parent.row
	e := Box(c).Background(c.theme.Border).Shrink(0).AlignSelf(Stretch)
	if row {
		return e.Width(1)
	}
	return e.Height(1)
}

// Animate returns a value that moves to target over d, easing out, and
// keeps frames coming while it moves; key tells apart the animations of
// the element. The value starts at the first target.
func (e *Element) Animate(key any, target float32, d time.Duration) float32 {
	st := e.st
	if st.anims == nil {
		st.anims = map[any]*anim{}
	}
	a := st.anims[key]
	now := e.c.now
	if a == nil {
		st.anims[key] = &anim{from: target, to: target, value: target}
		return target
	}
	if a.to != target {
		a.from, a.to, a.start, a.dur = a.value, target, now, d
	}
	if a.value != a.to {
		t := float32(now.Sub(a.start)) / float32(max(a.dur, time.Millisecond))
		if t >= 1 {
			a.value = a.to
		} else {
			u := 1 - t
			a.value = a.from + (a.to-a.from)*(1-u*u*u)
			e.c.AnimationFrame()
		}
	}
	return a.value
}

type anim struct {
	from, to, value float32
	start           time.Time
	dur             time.Duration
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// Button creates a button showing label. Ask Clicked whether it was
// clicked; give it other content with Children and an empty label.
func Button(c *Context, label string) *Element { return button(c, label, false) }

// PrimaryButton creates a button in the accent color, for the main action.
func PrimaryButton(c *Context, label string) *Element { return button(c, label, true) }

func button(c *Context, label string, primary bool) *Element {
	b := ButtonBase(c)
	styleButton(c, b, primary)
	if label != "" {
		b.Children(func() { Text(c, label).SingleLine() })
	}
	return b
}

// styleButton gives a button the theme's look, in the accent color when
// primary.
func styleButton(c *Context, b *Element, primary bool) {
	t := c.theme
	b.Padding(t.space(1.5), t.space(3.5)).Gap(t.space(1.5)).Radius(t.Radius)
	base, hover, pressed, fg, border := t.Surface, t.SurfaceHover, t.SurfacePressed, t.Text, t.Border
	if primary {
		base, hover, pressed, fg, border = t.Accent, t.AccentHover, t.AccentPressed, t.AccentText, Color{}
	}
	b.Background(base).TextColor(fg)
	if border.A > 0 {
		b.Border(1, border)
	}
	b.styleFn = func(b *Element) {
		if b.bg != base || b.IsDisabled() {
			return
		}
		if b.Pressed() {
			b.bg = pressed
		} else if b.Hovered() {
			b.bg = hover
		}
	}
}

// Link creates a text that opens url in the browser when clicked.
func Link(c *Context, label, url string) *Element {
	t := c.theme
	e := Text(c, label).TextColor(t.Accent).Cursor(CursorPointer).Focusable()
	e.widget, e.role = "Link", RoleLink
	if e.Clicked() && url != "" {
		c.rt.host.openURL(url)
	}
	if e.Hovered() {
		e.Underline()
	}
	return e
}

func checkPath(r Rect) *Path {
	var p Path
	return p.MoveTo(r.X+r.W*0.22, r.Y+r.H*0.52).LineTo(r.X+r.W*0.42, r.Y+r.H*0.71).LineTo(r.X+r.W*0.78, r.Y+r.H*0.31)
}

// Checkbox creates a check box toggling *checked, with a label.
func Checkbox(c *Context, checked *bool, label string) *Element {
	t := c.theme
	row := CheckboxBase(c, checked).Gap(t.space(2)).FocusRing(false)
	on := *checked
	row.Children(func() {
		box := Box(c).Size(t.space(4), t.space(4)).Radius(t.space(1)).Shrink(0)
		if on {
			box.Background(t.Accent)
		} else {
			box.Background(t.Background).Border(1, t.Border.Mix(t.Text, 0.25))
		}
		box.DrawOver(func(p *Painter, r Rect) {
			if on {
				p.StrokePath(checkPath(r), t.space(0.5), t.AccentText)
			}
			if row.FocusVisible() {
				p.FocusRing(r, box.radius)
			}
		})
		box.styleFn = func(box *Element) {
			if !on && row.Hovered() {
				box.borderC = t.Accent
			}
		}
		if label != "" {
			Text(c, label)
		}
	})
	return row
}

// Radio creates a radio button that selects value into *selected, with a
// label.
func Radio[T comparable](c *Context, selected *T, value T, label string) *Element {
	t := c.theme
	row := RadioBase(c, selected, value).Gap(t.space(2)).FocusRing(false)
	on := *selected == value
	row.Children(func() {
		dot := Box(c).Size(t.space(4), t.space(4)).Radius(t.space(2)).Shrink(0)
		if on {
			dot.Background(t.Accent)
		} else {
			dot.Background(t.Background).Border(1, t.Border.Mix(t.Text, 0.25))
		}
		dot.DrawOver(func(p *Painter, r Rect) {
			if on {
				d := t.space(1.5)
				p.Fill(Rect{r.X + (r.W-d)/2, r.Y + (r.H-d)/2, d, d}, t.AccentText, d/2)
			}
			if row.FocusVisible() {
				p.FocusRing(r, dot.radius)
			}
		})
		dot.styleFn = func(dot *Element) {
			if !on && row.Hovered() {
				dot.borderC = t.Accent
			}
		}
		if label != "" {
			Text(c, label)
		}
	})
	return row
}

// Switch creates a switch toggling *on.
func Switch(c *Context, on *bool) *Element {
	t := c.theme
	sw := SwitchBase(c, on).Size(t.space(9), t.space(5)).Radius(t.space(2.5))
	pos := sw.Animate("knob", b2f(*on), 140*time.Millisecond)
	off := t.Border.Mix(t.Text, 0.15)
	sw.Background(off.Mix(t.Accent, pos))
	sw.Draw(func(p *Painter, r Rect) {
		in := t.space(0.5)
		d := r.H - 2*in
		knob := Rect{r.X + in + pos*(r.W-r.H), r.Y + in, d, d}
		p.Shadow(Rect{knob.X, knob.Y + 1, knob.W, knob.H}, d/2, 3, RGBA(0, 0, 0, 0.25))
		p.Fill(knob, RGB(255, 255, 255), d/2)
	})
	return sw
}

// Slider creates a slider setting *value between lo and hi.
func Slider(c *Context, value *float64, lo, hi float64) *Element {
	t := c.theme
	// The knob moves across the content box, half of it inside the
	// padding on each side.
	knob := t.space(4)
	s := SliderBase(c, value, lo, hi).Height(t.space(5)).MinWidth(t.space(20)).PaddingX(knob / 2).FocusRing(false)
	frac := float32(0)
	if hi > lo {
		frac = float32((*value - lo) / (hi - lo))
	}
	s.Draw(func(p *Painter, r Rect) {
		h := t.space(1)
		track := Rect{r.X + knob/2, r.Y + r.H/2 - h/2, r.W - knob, h}
		p.Fill(track, t.Border.Mix(t.Text, 0.1), h/2)
		p.Fill(Rect{track.X, track.Y, track.W * frac, h}, t.Accent, h/2)
		k := Rect{r.X + (r.W-knob)*frac, r.Y + r.H/2 - knob/2, knob, knob}
		p.Shadow(Rect{k.X, k.Y + 1, k.W, k.H}, knob/2, 3, RGBA(0, 0, 0, 0.3))
		p.Fill(k, RGB(255, 255, 255), knob/2)
		p.Stroke(k, t.Border, knob/2, 1)
		if s.FocusVisible() {
			p.FocusRing(k, [4]float32{knob / 2, knob / 2, knob / 2, knob / 2})
		}
	})
	return s
}

// Progress creates a progress bar filled to value between 0 and 1; a
// negative value shows activity of unknown length.
func Progress(c *Context, value float64) *Element {
	t := c.theme
	rad := t.space(0.75)
	e := Box(c).Height(t.space(1.5)).Radius(rad).Background(t.Border).Clip()
	e.role, e.hasRange, e.accRange = RoleProgress, true, [3]float64{0, 1, value}
	now := c.now
	if value < 0 {
		c.AnimationFrame()
	}
	e.Draw(func(p *Painter, r Rect) {
		if value >= 0 {
			p.Fill(Rect{r.X, r.Y, r.W * float32(math.Min(value, 1)), r.H}, t.Accent, rad)
			return
		}
		phase := float32(now.UnixMilli()%1400) / 1400
		w := r.W * 0.3
		x := r.X - w + (r.W+w)*phase
		p.Clip(r, rad, func() { p.Fill(Rect{x, r.Y, w, r.H}, t.Accent, rad) })
	})
	return e
}

// Scroll creates a container that scrolls its children vertically. Give
// it a size, or Grow it within its parent.
func Scroll(c *Context) *Element {
	e := Box(c)
	e.flags |= flagScrollY | flagHover
	return e
}

// ScrollHorizontal creates a row that scrolls its children horizontally.
func ScrollHorizontal(c *Context) *Element {
	e := Row(c)
	e.flags |= flagScrollX | flagHover
	return e
}

// List creates a vertical scroll container for n rows of rowHeight DIPs
// that only builds the rows in view, with row(i).
func List(c *Context, n int, rowHeight float32, row func(i int)) *Element {
	e := Scroll(c)
	e.widget, e.role = "List", RoleList
	st := e.st
	view := st.h
	if view <= 0 {
		view = c.h
	}
	first := max(0, int(st.scrollY/rowHeight)-2)
	last := min(n, int((st.scrollY+view)/rowHeight)+3)
	e.Children(func() {
		if first > 0 {
			Box(c).Height(float32(first) * rowHeight).Shrink(0)
		}
		for i := first; i < last; i++ {
			r := Box(c).Key(i).Height(rowHeight).Shrink(0)
			r.Children(func() { row(i) })
		}
		if last < n {
			Box(c).Height(float32(n-last) * rowHeight).Shrink(0)
		}
	})
	return e
}

// Bitmap is an image to show with Image. Create it once: converting an
// image is not free.
type Bitmap struct {
	img  *scene.Image
	w, h int
}

// NewBitmap converts img.
func NewBitmap(img image.Image) *Bitmap {
	s := scene.NewImage(img)
	return &Bitmap{img: s, w: s.W, h: s.H}
}

// DecodeBitmap decodes a PNG, JPEG or GIF image.
func DecodeBitmap(data []byte) (*Bitmap, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return NewBitmap(img), nil
}

// Size returns the bitmap's size in pixels, which Image shows as DIPs.
func (b *Bitmap) Size() (w, h int) { return b.w, b.h }

func (b *Bitmap) imageSize() (float32, float32) {
	if b == nil {
		return 0, 0
	}
	return float32(b.w), float32(b.h)
}

// ImageSource is what Image shows: a *Bitmap, or an *SVG in its own
// colors.
type ImageSource interface {
	imageSize() (w, h float32)
}

// Image creates an element showing a bitmap, or an SVG in its own colors
// (with the text color for its currentColor), by default at its size as
// DIPs, scaled to fit when given another size.
func Image(c *Context, src ImageSource) *Element {
	e := c.newElement(kindImage)
	switch s := src.(type) {
	case *Bitmap:
		e.image = s
	case *SVG:
		e.svg = s
	}
	if src != nil {
		if w, h := src.imageSize(); h > 0 {
			e.aspect = w / h
		}
	}
	return e
}

// intrinsicSize returns the size of an image's picture, or of an icon: as
// high as the font size.
func (e *Element) intrinsicSize() (w, h float32) {
	switch e.kind {
	case kindImage:
		if e.image != nil {
			return e.image.imageSize()
		}
		return e.svg.imageSize()
	case kindIcon:
		em := e.resolvedText().size
		if s := e.svg; s != nil && s.h > 0 {
			return em * s.w / s.h, em
		}
		return em, em
	}
	return 0, 0
}

// Fit sets how an Image fills its box.
func (e *Element) Fit(f Fit) *Element { e.fit = f; return e }

// Tooltip shows s near the pointer when it rests on the element.
func (e *Element) Tooltip(s string) *Element {
	e.flags |= flagHover
	rt := e.c.rt
	if !e.Hovered() || rt.pressed != nil || s == "" {
		return e
	}
	// Only the innermost element with a tooltip shows it.
	if rt.tooltipFrame == rt.frame && rt.tooltipDepth >= e.depth {
		return e
	}
	rt.tooltipFrame, rt.tooltipDepth = rt.frame, e.depth
	wait := 600*time.Millisecond - e.c.now.Sub(rt.hoverSince)
	if wait > 0 {
		e.c.After(wait)
		return e
	}
	c := e.c
	t := c.theme
	x, y := rt.pointerX+12, rt.pointerY+18
	Overlay(c, func() {
		tip := Box(c).Absolute().Left(x).Top(y).MaxWidth(t.space(80)).Padding(t.space(1.25), t.space(2)).Radius(t.space(1.25)).
			Background(t.Text).TextColor(t.Background).FontSize(t.FontSize - 1).PassThrough().Role(RoleTooltip)
		tip.Shadow(0, 2, 8, 0, RGBA(0, 0, 0, 0.2))
		tip.Children(func() { Text(c, s) })
		keepInWindow(tip, x, y, y-30)
	})
	return e
}

// Overlay builds fn's elements above the rest of the window. Place them
// with Absolute, Left and Top, in DIPs relative to the window.
func Overlay(c *Context, fn func()) {
	saved := c.parent
	c.parent = c.overlayRoot()
	fn()
	c.parent = saved
}

// Modal shows a dialog built by fn over a dimmed window while *open is
// true; clicking outside it or pressing Escape sets *open to false.
func Modal(c *Context, open *bool, fn func()) *Element {
	if !*open {
		return nil
	}
	t := c.theme
	return DialogBase(c, open, func(back, panel *Element) {
		back.Background(RGBA(0, 0, 0, 0.4))
		panel.Padding(t.space(5)).Gap(t.space(3)).Radius(t.space(2.5)).Background(t.Background).MaxWidth(c.w - t.space(10)).MaxHeight(c.h - t.space(10))
		panel.Shadow(0, 10, 30, 0, RGBA(0, 0, 0, 0.3))
		fn()
	})
}

// Popover shows fn's elements in a panel below anchor while *open is
// true; clicking outside it or pressing Escape sets *open to false.
func Popover(c *Context, anchor *Element, open *bool, fn func()) *Element {
	if !*open {
		return nil
	}
	return PopoverBase(c, anchor, open, func(panel *Element) {
		panel.MinWidth(anchor.Bounds().W)
		stylePanel(c, panel)
		fn()
	})
}

// stylePanel gives the panel of a popup the theme's look.
func stylePanel(c *Context, panel *Element) {
	t := c.theme
	panel.Margin(t.space(1), 0, 0, 0).Padding(t.space(1)).Radius(t.Radius+2).Background(t.Background).Border(1, t.Border)
	panel.Shadow(0, 6, 20, 0, RGBA(0, 0, 0, 0.18))
}

// Select creates a drop-down choosing one of options into *selected.
func Select(c *Context, selected *string, options []string) *Element {
	t := c.theme
	sel := SelectBase(c, selected)
	b := sel.Trigger
	styleButton(c, b, false)
	b.Justify(SpaceBetween).MinWidth(t.space(35))
	b.Children(func() {
		Text(c, *selected).SingleLine()
		Box(c).Size(t.space(2.5), t.space(2.5)).Shrink(0).Draw(func(p *Painter, r Rect) {
			var path Path
			path.MoveTo(r.X+r.W*0.1, r.Y+r.H*0.3).LineTo(r.X+r.W*0.5, r.Y+r.H*0.7).LineTo(r.X+r.W*0.9, r.Y+r.H*0.3)
			p.StrokePath(&path, 1.5, t.TextMuted)
		})
	})
	sel.Popup(func(panel *Element) {
		stylePanel(c, panel)
		for _, opt := range options {
			item := sel.Item(opt).Padding(t.space(1.5), t.space(2.5)).Radius(t.Radius)
			switch {
			case item.Highlighted():
				item.Background(t.Accent).TextColor(t.AccentText)
			case opt == *selected:
				item.Background(t.Surface)
			}
			item.Children(func() { Text(c, opt).SingleLine() })
		}
	})
	return b
}

// keepInWindow places an overlay element at (x, y), where the layout,
// which knows its size, moves it to fit in the window: left when it would
// overflow the right edge, above, ending at aboveY, when it would
// overflow the bottom. A top margin keeps it apart from what it is above
// or below.
func keepInWindow(e *Element, x, y, aboveY float32) {
	e.Left(x).Top(y)
	e.place = placement{on: true, above: aboveY}
}

// placement is where an overlay element goes when it does not fit below
// what it belongs to: above, its bottom at above.
type placement struct {
	on    bool
	above float32
}

// fit moves an absolute element w×h at (left, top) in a containing block
// pw×ph, its placement says, to fit in the block.
func (p placement) fit(e *Element, left, top, w, h, pw, ph float32) (float32, float32) {
	if left+e.margin[3]+w > pw-4 {
		left = max(4-e.margin[3], pw-4-w-e.margin[3])
	}
	m := e.margin[0]
	if top+m+h > ph-4 && p.above-m-h > 4 {
		top = p.above - h - 2*m
	}
	return left, top
}
