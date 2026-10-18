package ui

import (
	"slices"

	"github.com/egoist/mygo/internal/text"
)

// The layout cache lays out again only the elements whose layout may
// have changed since the frame before, and gives the others the boxes
// they had then, as browsers and Flutter do.
//
// A retained tree knows what changed as it changes: Blink marks a box
// needing layout, and its ancestors as having a child that does, and
// reuses the layout result of a box not marked when its constraint space
// is the one it was laid out in; Flutter skips a RenderObject whose
// constraints are those of its last layout unless it was marked; Taffy
// keeps the sizes of each node by the sizes known and the room given,
// and marking a node dirty clears its and its ancestors'. The view builds
// MyGo's tree anew each frame, so nothing marks what changed: comparing
// each element with what its state kept of it in the frame before finds
// it instead (sameLayout), before laying out. An element is the same when
// what lays it out is (its layoutStyle, the flags and the text styles the
// layout reads, its grid's tracks, its spans, its image's size), its
// children are the same elements in the same order, each the same in
// turn, and its ancestors' text styles, which it inherits, are the same.
// That is Blink's NeedsLayout and ChildNeedsLayout, found by comparison.
//
// layoutBox then reuses what an element that is the same laid out in the
// frame before when it lays it out in a box of the same size (its
// constraint space, which percentages resolve against): its text's
// layout, its children's boxes, and theirs in turn, laying out again any
// child the frame before laid out at another size. The sizes measured of
// it (measured: its max-content and min-content widths, its heights at
// widths) are those of the frame before too, as Blink's intrinsic sizes
// and Taffy's measure cache are, so a container laid out again measures
// the children that are the same at once.
//
// Some elements lay out by more than what they are built with: a List
// builds its rows as it lays out, by its scroll offset; a Form aligns its
// labels, a Table fits its columns and a toolbar collapses its controls,
// changing elements inside them; an input lays out its text by its
// editor; an element attached to another is placed by that one's box.
// Those are never the same (volatile), nor anything around them, nor
// anything inside those that change what is inside them. Transforms, as
// Translate, and the scroll offsets of other containers move elements
// after layout (place, commit) and are not compared.
//
// TestLayoutCacheMatches lays random trees out with and without the
// cache, changing them at random frame after frame.

const (
	// layOwner: the element alone has its state in this layout, and keeps
	// what it lays out there.
	layOwner uint8 = 1 << iota
	// layReuse: the element and those inside it are as in the last layout
	// (sameLayout), whose boxes layoutBox may reuse.
	layReuse
)

// layoutFlags are the flags that the layout reads.
const layoutFlags = flagScrollX | flagScrollY | flagClipX | flagClipY | flagAbsolute | flagSelectable | flagInvisible

// textLayoutSet are the text styles that lay text out (textParams) and
// size icons, which elements inherit.
const textLayoutSet = setFamily | setSize | setWeight | setItalic | setLineHeight | setAlign | setSpacing | setFeatures | setLang

// layoutRecord is what an element's state keeps of its last layout.
type layoutRecord struct {
	// layout is engine.layouts of the last layout that saw the element,
	// and ok tells that what follows, to parent, is what it saw then.
	layout     uint64
	ok         bool
	style      layoutStyle
	flags      uint32
	ts         textLayout
	cols, rows []Track
	spans      string
	media      [2]float32
	placeOn    uint64
	// parent is the ID of its parent, index its place among its siblings
	// and nchild how many children it had.
	parent        uint64
	index, nchild int32

	// What was measured of it, when measuredOK, and, when sized, how
	// layoutBox laid it out at w×h.
	measured
	measuredOK         bool
	sized              bool
	w, h               float32
	tl                 *text.Layout
	contentW, contentH float64
	// box is the box its parent gave it, relative to the parent, which
	// laid it out in it when boxed.
	box   Rect
	boxed bool
}

// beginLayout finds the elements of the frame that lay out as in the last
// one (sameLayout).
func (rt *engine) beginLayout(root *node) {
	rt.layouts++
	// The text system let go of the layouts and fonts it had, or the
	// cache is off: lay everything out anew.
	changed := rt.noLayoutCache || !rt.layGenOK || rt.layGen != rt.gen
	rt.layGen, rt.layGenOK = rt.gen, true
	sameLayout(root, 0, 0, changed)
}

// sameLayout reports whether e, the index-th child of the element parent,
// and the elements inside it lay out as in the last layout, keeping what
// they are for the next. changed tells that something around it that it
// inherits changed.
func sameLayout(e *node, parent uint64, index int, changed bool) bool {
	r := &e.st.lay
	n := e.c.rt.layouts
	if r.layout == n {
		// Another element of this layout has its state, as two with one
		// key: neither keeps its layout there, nor reuses what is there.
		r.ok, r.sized, r.boxed, r.measuredOK = false, false, false, false
		e.lay = 0
		for c, i := e.first, 0; c != nil; c, i = c.next, i+1 {
			sameLayout(c, e.id, i, true)
		}
		return false
	}
	e.lay = layOwner
	var spans string
	if e.kind == kindText && e.spans != nil && e.first == nil {
		spans = e.textSpans()
	}
	var media [2]float32
	switch e.kind {
	case kindImage:
		if e.image != nil {
			media[0], media[1] = e.image.imageSize()
		} else {
			media[0], media[1] = e.svg.imageSize()
		}
	case kindIcon:
		media[0], media[1] = e.svg.imageSize()
	}
	var placeOn uint64
	if e.placeOn != nil {
		placeOn = e.placeOn.id
	}
	// What lays it out, against what the last layout saw.
	fresh := r.ok && r.layout == n-1
	sameText := fresh && r.ts.same(&e.ts)
	if !sameText {
		r.ts = layoutOfText(&e.ts)
	}
	equal := fresh && r.style == e.layoutStyle && r.flags == e.flags&layoutFlags &&
		r.spans == spans && r.media == media && r.placeOn == placeOn &&
		r.parent == parent && r.index == int32(index) && r.nchild == int32(e.nchild) &&
		slices.Equal(r.cols, e.cols) && slices.Equal(r.rows, e.rows)
	if !equal {
		r.ok = true
		r.style, r.flags = e.layoutStyle, e.flags&layoutFlags
		if !slices.Equal(r.cols, e.cols) || !slices.Equal(r.rows, e.rows) {
			r.cols, r.rows = append(r.cols[:0], e.cols...), append(r.rows[:0], e.rows...)
		}
		r.spans, r.media, r.placeOn = spans, media, placeOn
		r.parent, r.index, r.nchild = parent, int32(index), int32(e.nchild)
	}
	r.layout = n
	same := equal && sameText && !changed

	// Elements that lay out by more than they are built with, and those
	// that change what is inside them as they lay out, whose elements
	// keep nothing: the next layout finds their states not seen by this
	// one.
	self := e.kind == kindInput || e.attach != 0 || e.ghosts
	tree := e.list != nil || e.form != nil || e.colFit != nil || e.overflow != nil ||
		e.gridFit != nil || e.leaving != 0 || e.kind == kindText && e.first != nil
	if tree {
		keepNoLayout(e)
	} else {
		inner := changed || !sameText
		for c, i := e.first, 0; c != nil; c, i = c.next, i+1 {
			if !sameLayout(c, e.id, i, inner) {
				same = false
			}
		}
	}
	if !same || self || tree {
		r.sized, r.boxed, r.measuredOK = false, false, false
		return false
	}
	e.lay |= layReuse
	if r.measuredOK {
		e.measured = r.measured
	}
	return true
}

// keepNoLayout makes the elements inside e keep and reuse no layout.
func keepNoLayout(e *node) {
	for c := e.first; c != nil; c = c.next {
		c.lay = 0
		keepNoLayout(c)
	}
}

// textLayout is what of a text style lays text out (textLayoutSet), and
// passes to the elements inside.
type textLayout struct {
	set                       uint32
	family, features, lang    string
	size, lineHeight, spacing float32
	weight                    int
	italic, fixedLine         bool
	align                     Align
}

// same reports whether t lays text out as l says.
func (l *textLayout) same(t *textStyle) bool {
	set := t.set & textLayoutSet
	if set != l.set {
		return false
	}
	if set == 0 {
		return true
	}
	return (set&setFamily == 0 || l.family == t.family) &&
		(set&setSize == 0 || l.size == t.size) &&
		(set&setWeight == 0 || l.weight == t.weight) &&
		(set&setItalic == 0 || l.italic == t.italic) &&
		(set&setLineHeight == 0 || l.lineHeight == t.lineHeight && l.fixedLine == t.fixedLine) &&
		(set&setAlign == 0 || l.align == t.align) &&
		(set&setSpacing == 0 || l.spacing == t.spacing) &&
		(set&setFeatures == 0 || l.features == t.features) &&
		(set&setLang == 0 || l.lang == t.lang)
}

// layoutOfText returns what of t lays text out.
func layoutOfText(t *textStyle) textLayout {
	set := t.set & textLayoutSet
	if set == 0 {
		return textLayout{}
	}
	l := textLayout{set: set}
	if set&setFamily != 0 {
		l.family = t.family
	}
	if set&setSize != 0 {
		l.size = t.size
	}
	if set&setWeight != 0 {
		l.weight = t.weight
	}
	if set&setItalic != 0 {
		l.italic = t.italic
	}
	if set&setLineHeight != 0 {
		l.lineHeight, l.fixedLine = t.lineHeight, t.fixedLine
	}
	if set&setAlign != 0 {
		l.align = t.align
	}
	if set&setSpacing != 0 {
		l.spacing = t.spacing
	}
	if set&setFeatures != 0 {
		l.features = t.features
	}
	if set&setLang != 0 {
		l.lang = t.lang
	}
	return l
}

// keepMeasured keeps what was measured of e for the next layout.
func (e *node) keepMeasured() {
	if e.lay&layOwner != 0 {
		r := &e.st.lay
		r.measured, r.measuredOK = e.measured, true
	}
}

// reuseBox lays e out at the size of its last layout as it was then
// (layReuse).
func reuseBox(e *node, r *layoutRecord) {
	e.w, e.h = r.w, r.h
	switch e.kind {
	case kindText:
		e.tl = r.tl
		if ed := e.st.editor; ed != nil && e.flags&flagSelectable != 0 {
			ed.layout = e.tl
			ed.originX, ed.originY = e.contentX(), e.contentY()
		}
		return
	case kindImage, kindIcon:
		return
	}
	e.contentW, e.contentH = r.contentW, r.contentH
	for c := e.first; c != nil; c = c.next {
		b := &c.st.lay
		c.x, c.y = b.box.X, b.box.Y
		if b.boxed {
			layoutBox(c, b.box.W, b.box.H)
		} else {
			c.w, c.h = b.box.W, b.box.H
		}
	}
}

// keepBox keeps how e was laid out at w×h, by the layoutBox that began at
// start, and its children's boxes.
func keepBox(e *node, w, h float32, start uint64) {
	r := &e.st.lay
	r.sized, r.w, r.h = true, w, h
	r.tl = e.tl
	r.contentW, r.contentH = e.contentW, e.contentH
	r.measured, r.measuredOK = e.measured, true
	for c := e.first; c != nil; c = c.next {
		if c.lay&layOwner != 0 {
			b := &c.st.lay
			b.box, b.boxed = Rect{c.x, c.y, c.w, c.h}, c.laid > start
		}
	}
}
