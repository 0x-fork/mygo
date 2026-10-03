package ui

import (
	"strings"

	"github.com/egoist/mygo/internal/text"
)

// Align positions children along an axis: Justify places them along the
// main axis (the direction of a Row or Column), AlignItems and AlignSelf
// across it.
type Align uint8

const (
	// Start is the left of a row, the top of a column.
	Start Align = iota
	Center
	End
	// Stretch makes children as large as the container across its axis.
	Stretch
	// SpaceBetween, SpaceAround and SpaceEvenly spread children along the
	// main axis (Justify only).
	SpaceBetween
	SpaceAround
	SpaceEvenly
	alignAuto
)

type unit uint8

const (
	unitAuto unit = iota
	unitPx
	unitPercent
)

// length is a size in DIPs, a percentage of the parent's or automatic.
type length struct {
	v float32
	u unit
}

func px(v float32) length      { return length{v, unitPx} }
func percent(v float32) length { return length{v, unitPercent} }

// resolve returns the length in DIPs against base, and false for auto or a
// percentage of an unknown base.
func (l length) resolve(base float32) (float32, bool) {
	switch l.u {
	case unitPx:
		return l.v, true
	case unitPercent:
		if base >= 0 {
			return l.v / 100 * base, true
		}
	}
	return 0, false
}

type kind uint8

const (
	kindBox kind = iota
	kindText
	kindImage
	kindInput
	kindIcon
)

// flags of an element.
const (
	flagClickable uint32 = 1 << iota
	flagFocusable
	flagEditable
	flagDragWindow
	flagScrollX
	flagScrollY
	flagClip
	flagAbsolute
	flagDisabled
	flagTrackPointer
	flagPassThrough
	flagDraggable
	flagHover
	flagOwnRing
	flagDropTarget
	flagContextMenu
	flagSelectable
)

type shadow struct {
	x, y, blur, spread float32
	color              Color
}

// textStyle is the text styling of an element; descendants inherit what is
// set.
type textStyle struct {
	set        uint16
	family     string
	size       float32
	weight     int
	italic     bool
	color      Color
	lineHeight float32
	align      Align
	underline  bool
	strike     bool
	spacing    float32 // letter spacing
	features   string
}

const (
	setFamily uint16 = 1 << iota
	setSize
	setWeight
	setItalic
	setColor
	setLineHeight
	setAlign
	setUnderline
	setStrike
	setSpacing
	setFeatures

	// setAll has every bit of textStyle.set.
	setAll = setFeatures<<1 - 1
)

// Element is a node of a frame's user interface. The functions that create
// elements return them so that their methods can style them and ask about
// their interaction, and the methods return the element for chaining:
//
//	ui.Text(c, "Hello").FontSize(20).Bold()
//
// An element only lives during the frame that built it.
type Element struct {
	c      *Context
	id     uint64
	kind   kind
	flags  uint32
	parent *Element
	first  *Element
	last   *Element
	next   *Element
	nchild int
	depth  int
	st     *state

	// Layout.
	row                    bool
	wrap                   bool
	justify, align, self   Align
	gap                    float32
	pad, margin            [4]float32 // top, right, bottom, left
	width, height          length
	minW, minH, maxW, maxH length
	grow, shrink           float32
	basis                  length
	inset                  [4]length
	aspect                 float32

	// Painting.
	bg, bg2      Color
	gradAngle    float32
	hasGrad      bool
	borderW      float32
	borderC      Color
	radius       [4]float32
	shadows      []shadow
	opacity      float32
	opacitySet   bool
	cursor       Cursor
	paintFn      func(p *Painter, r Rect)
	styleFn      func(e *Element)
	paintAfterFn func(p *Painter, r Rect)

	// Content.
	text     string
	spans    []Span // of a RichText
	spansKey string // their styles, for the layout
	ts       textStyle
	maxLines int
	single   bool
	image    *Bitmap
	svg      *SVG // of an Icon, or an Image in its own colors
	fit      Fit
	label    string
	// widget names the widget that used the element's state as it created
	// it, which Key would then lose.
	widget string

	// What assistive technology sees: the role, whether a check box,
	// radio or switch is off (1), on (2) or mixed (3), whether a pop-up
	// shows, a value, and a range's minimum, maximum and value.
	role     Role
	checked  int8
	expanded bool
	// highlighted is set on the option of a select the pointer or the
	// arrows are on.
	highlighted bool
	// place moves an overlay element to fit in the window, as keepInWindow
	// asked.
	place    placement
	accValue string
	accRange [3]float64
	hasRange bool

	// Layout results, in DIPs relative to the window.
	x, y, w, h float32
	contentW   float32
	contentH   float32
	tl         *text.Layout
	measures   [4]measure
	nmeasure   int
}

type measure struct {
	availW, availH, w, h float32
}

// Rect is a rectangle in DIPs.
type Rect struct{ X, Y, W, H float32 }

// Contains reports whether the point is inside r.
func (r Rect) Contains(x, y float32) bool { return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H }

// Fit says how an image fills its element.
type Fit uint8

const (
	// Contain scales the image to fit inside the element, keeping its
	// aspect ratio.
	Contain Fit = iota
	// Cover scales the image to cover the element, cropping it.
	Cover
	// FillBox stretches the image to the element.
	FillBox
)

// Children builds the element's children: elements created while fn runs
// are added to e.
func (e *Element) Children(fn func()) *Element {
	c := e.c
	saved := c.parent
	c.parent = e
	fn()
	c.parent = saved
	return e
}

// Key identifies the element among its siblings by k instead of by its
// position, so that its state (focus, scrolling, text being edited, …)
// follows it when the siblings before it change. Call it right after
// creating the element. Widgets that handle their input as they are
// created (Checkbox, Radio, Switch, Slider, Select, Link, List, TextInput
// and TextArea) cannot take a key, and Key panics: give it to an element
// around them instead, as Row(c).Key(k).Children(...) does.
func (e *Element) Key(k any) *Element {
	if e.widget != "" {
		panic("ui: Key on a " + e.widget + ", which handles its input as it is created: give the key to an element around it")
	}
	e.c.rekey(e, k)
	return e
}

// Row lays the children out from left to right.
func (e *Element) Row() *Element {
	e.row = true
	if e.align == alignAuto {
		e.align = Center
	}
	return e
}

// Column lays the children out from top to bottom.
func (e *Element) Column() *Element { e.row = false; return e }

// Wrap starts a new line of children when they do not fit.
func (e *Element) Wrap() *Element { e.wrap = true; return e }

// Gap puts space between children.
func (e *Element) Gap(v float32) *Element { e.gap = v; return e }

// edges expands CSS shorthand values: all, vertical horizontal, top
// horizontal bottom, or top right bottom left.
func edges(v []float32) [4]float32 {
	switch len(v) {
	case 1:
		return [4]float32{v[0], v[0], v[0], v[0]}
	case 2:
		return [4]float32{v[0], v[1], v[0], v[1]}
	case 3:
		return [4]float32{v[0], v[1], v[2], v[1]}
	case 4:
		return [4]float32{v[0], v[1], v[2], v[3]}
	}
	return [4]float32{}
}

// Padding sets the space inside the element's edges, CSS style: all
// sides, vertical and horizontal, or top, right, bottom and left.
func (e *Element) Padding(v ...float32) *Element { e.pad = edges(v); return e }

// PaddingX sets the left and right padding.
func (e *Element) PaddingX(v float32) *Element { e.pad[1], e.pad[3] = v, v; return e }

// PaddingY sets the top and bottom padding.
func (e *Element) PaddingY(v float32) *Element { e.pad[0], e.pad[2] = v, v; return e }

// Margin sets the space around the element, as Padding does.
func (e *Element) Margin(v ...float32) *Element { e.margin = edges(v); return e }

// Width sets the width in DIPs.
func (e *Element) Width(v float32) *Element { e.width = px(v); return e }

// Height sets the height in DIPs.
func (e *Element) Height(v float32) *Element { e.height = px(v); return e }

// Size sets the width and height in DIPs.
func (e *Element) Size(w, h float32) *Element { e.width, e.height = px(w), px(h); return e }

// WidthPercent sets the width as a percentage of the parent's.
func (e *Element) WidthPercent(p float32) *Element { e.width = percent(p); return e }

// HeightPercent sets the height as a percentage of the parent's.
func (e *Element) HeightPercent(p float32) *Element { e.height = percent(p); return e }

// FillWidth makes the element as wide as its parent's content.
func (e *Element) FillWidth() *Element { e.width = percent(100); return e }

// FillHeight makes the element as tall as its parent's content.
func (e *Element) FillHeight() *Element { e.height = percent(100); return e }

// Fill makes the element as large as its parent's content.
func (e *Element) Fill() *Element { e.width, e.height = percent(100), percent(100); return e }

// MinWidth, MinHeight, MaxWidth and MaxHeight bound the size in DIPs.
func (e *Element) MinWidth(v float32) *Element  { e.minW = px(v); return e }
func (e *Element) MinHeight(v float32) *Element { e.minH = px(v); return e }
func (e *Element) MaxWidth(v float32) *Element  { e.maxW = px(v); return e }
func (e *Element) MaxHeight(v float32) *Element { e.maxH = px(v); return e }

// Grow gives the element a share f of the free space along its parent's
// main axis: Grow(1) on one child makes it take all of it. Like CSS flex:
// f, the element then starts from no size (unless Basis says otherwise) and
// may shrink below its content in a column, which suits a list or editor
// filling the rest of a window.
func (e *Element) Grow(f float32) *Element {
	e.grow = f
	if e.basis.u == unitAuto {
		e.basis = px(0)
	}
	return e
}

// Shrink sets how much the element gives up when its siblings do not fit
// (1 by default, 0 never).
func (e *Element) Shrink(f float32) *Element { e.shrink = f; return e }

// Basis sets the size along the parent's main axis before growing or
// shrinking.
func (e *Element) Basis(v float32) *Element { e.basis = px(v); return e }

// Justify places the children along the main axis.
func (e *Element) Justify(a Align) *Element { e.justify = a; return e }

// AlignItems places the children across the main axis.
func (e *Element) AlignItems(a Align) *Element { e.align = a; return e }

// AlignSelf places the element across its parent's main axis, overriding
// the parent's AlignItems.
func (e *Element) AlignSelf(a Align) *Element { e.self = a; return e }

// Center centers the children along and across the main axis.
func (e *Element) Center() *Element { e.justify, e.align = Center, Center; return e }

// Absolute takes the element out of its parent's layout and places it with
// Top, Right, Bottom and Left relative to the parent's padding box, above
// its siblings.
func (e *Element) Absolute() *Element { e.flags |= flagAbsolute; return e }

// Top, Right, Bottom and Left place an Absolute element.
func (e *Element) Top(v float32) *Element    { e.inset[0] = px(v); return e }
func (e *Element) Right(v float32) *Element  { e.inset[1] = px(v); return e }
func (e *Element) Bottom(v float32) *Element { e.inset[2] = px(v); return e }
func (e *Element) Left(v float32) *Element   { e.inset[3] = px(v); return e }

// AspectRatio makes the height the width divided by r.
func (e *Element) AspectRatio(r float32) *Element { e.aspect = r; return e }

// Clip hides what the children draw outside the element.
func (e *Element) Clip() *Element { e.flags |= flagClip; return e }

// Background fills the element.
func (e *Element) Background(c Color) *Element { e.bg = c; e.hasGrad = false; return e }

// Gradient fills the element with a linear gradient from one color to
// another, at angle degrees clockwise from upwards as in CSS: 180 goes
// from top to bottom, 90 from left to right.
func (e *Element) Gradient(from, to Color, angle float32) *Element {
	e.bg, e.bg2, e.gradAngle, e.hasGrad = from, to, angle, true
	return e
}

// Border draws a border of width DIPs inside the element's edges.
func (e *Element) Border(width float32, c Color) *Element { e.borderW, e.borderC = width, c; return e }

// Radius rounds the corners: one radius for all, or top-left, top-right,
// bottom-right and bottom-left.
func (e *Element) Radius(r ...float32) *Element {
	switch len(r) {
	case 1:
		e.radius = [4]float32{r[0], r[0], r[0], r[0]}
	case 4:
		e.radius = [4]float32{r[0], r[1], r[2], r[3]}
	}
	return e
}

// Shadow adds a box shadow, offset by x and y, blurred by blur and grown by
// spread DIPs.
func (e *Element) Shadow(x, y, blur, spread float32, c Color) *Element {
	e.shadows = append(e.shadows, shadow{x, y, blur, spread, c})
	return e
}

// Opacity makes the element and its children translucent.
func (e *Element) Opacity(o float32) *Element {
	e.opacity, e.opacitySet = max(0, min(o, 1)), true
	return e
}

// Cursor sets the pointer's shape over the element.
func (e *Element) Cursor(c Cursor) *Element { e.cursor = c + 1; return e }

// FontSize sets the size of text in DIPs, for the element's text and its
// descendants'.
func (e *Element) FontSize(v float32) *Element { e.ts.size = v; e.ts.set |= setSize; return e }

// FontWeight sets the weight of text from 100 (thin) to 900 (black).
func (e *Element) FontWeight(w int) *Element { e.ts.weight = w; e.ts.set |= setWeight; return e }

// Bold sets a bold font weight.
func (e *Element) Bold() *Element { return e.FontWeight(700) }

// Italic sets an italic font.
func (e *Element) Italic() *Element { e.ts.italic = true; e.ts.set |= setItalic; return e }

// Font sets the font family, a comma-separated list: the first family the
// system or the app has draws the text, and the others, in order, what it
// lacks, before the system's choice. "monospace" and "system-ui" are the
// system's own fonts.
func (e *Element) Font(family string) *Element { e.ts.family = family; e.ts.set |= setFamily; return e }

// TextColor sets the color of text.
func (e *Element) TextColor(c Color) *Element { e.ts.color = c; e.ts.set |= setColor; return e }

// LineHeight sets the height of lines of text as a multiple of the font
// size.
func (e *Element) LineHeight(m float32) *Element {
	e.ts.lineHeight = m
	e.ts.set |= setLineHeight
	return e
}

// TextAlign aligns the lines of text: Start, Center or End.
func (e *Element) TextAlign(a Align) *Element { e.ts.align = a; e.ts.set |= setAlign; return e }

// Underline underlines text.
func (e *Element) Underline() *Element { e.ts.underline = true; e.ts.set |= setUnderline; return e }

// Strikethrough strikes text through.
func (e *Element) Strikethrough() *Element { e.ts.strike = true; e.ts.set |= setStrike; return e }

// LetterSpacing adds v DIPs after every character of text, or tightens it
// with a negative v, as for labels in capitals.
func (e *Element) LetterSpacing(v float32) *Element {
	e.ts.spacing = v
	e.ts.set |= setSpacing
	return e
}

// FontFeatures turns on OpenType features of the font, by tag, or sets
// them with tag=value:
//
//	ui.Textf(c, "%d items", n).FontFeatures("tnum")   // digits of one width
//	ui.Text(c, "office").FontFeatures("liga=0")       // no ligatures
//
// A font without a feature ignores it.
func (e *Element) FontFeatures(features ...string) *Element {
	e.ts.features = strings.Join(features, ",")
	e.ts.set |= setFeatures
	return e
}

// MaxLines shows at most n lines of the element's text, ending it with an
// ellipsis.
func (e *Element) MaxLines(n int) *Element { e.maxLines = n; return e }

// SingleLine keeps the element's text on one line, ending it with an
// ellipsis when it does not fit.
func (e *Element) SingleLine() *Element { e.single = true; e.maxLines = 1; return e }

// Label names the element for assistive technology and for finding it in
// tests, when its text does not.
func (e *Element) Label(s string) *Element { e.label = s; return e }

// Disabled disables the element when d is true: it reports no clicks and
// widgets look disabled.
func (e *Element) Disabled(d bool) *Element {
	if d {
		e.flags |= flagDisabled
	} else {
		e.flags &^= flagDisabled
	}
	return e
}

// IsDisabled reports whether the element or an ancestor is disabled.
func (e *Element) IsDisabled() bool {
	for p := e; p != nil; p = p.parent {
		if p.flags&flagDisabled != 0 {
			return true
		}
	}
	return false
}

// Focusable lets the element take the keyboard focus, by a click or Tab.
func (e *Element) Focusable() *Element { e.flags |= flagFocusable; return e }

// DragWindow makes the element a handle that moves the window, such as the
// title bar of a frameless window. A double click on it maximizes the
// window, as on a title bar.
func (e *Element) DragWindow() *Element { e.flags |= flagDragWindow; return e }

// PassThrough lets the pointer reach what is under the element.
func (e *Element) PassThrough() *Element { e.flags |= flagPassThrough; return e }

// FocusRing sets whether MyGo rings the element when it has the keyboard
// focus from the keyboard, as it does by default. Widgets that ring a part
// of themselves instead, as a check box its box, turn it off and draw
// their own with Painter.FocusRing while FocusVisible.
func (e *Element) FocusRing(show bool) *Element {
	if show {
		e.flags &^= flagOwnRing
	} else {
		e.flags |= flagOwnRing
	}
	return e
}

// Draw paints on the element with p after its background, before its
// children; r is its box. fn only paints: it may run more than once a
// frame.
func (e *Element) Draw(fn func(p *Painter, r Rect)) *Element { e.paintFn = fn; return e }

// DrawOver paints on the element with p after its children.
func (e *Element) DrawOver(fn func(p *Painter, r Rect)) *Element { e.paintAfterFn = fn; return e }

// ID returns the element's identity, stable from frame to frame.
func (e *Element) ID() uint64 { return e.id }

// Bounds returns the element's box in the previous frame, in DIPs relative
// to the window; it is empty for an element the previous frame lacked.
func (e *Element) Bounds() Rect {
	s := e.st
	return Rect{s.x, s.y, s.w, s.h}
}

// add appends a child.
func (e *Element) add(child *Element) {
	child.parent = e
	child.depth = e.depth + 1
	if e.last == nil {
		e.first = child
	} else {
		e.last.next = child
	}
	e.last = child
	e.nchild++
}
