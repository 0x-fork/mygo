package ui

import (
	"math"

	"github.com/egoist/mygo/internal/text"
)

// The layout is CSS flexbox with box-sizing: border-box. Sizes are border
// boxes (padding and border included, margins not); inf marks a size not
// known yet, such as the height of a column that grows with its content.

var inf = float32(math.Inf(1))

func finite(v float32) bool { return v < inf }

func (e *Element) padX() float32    { return e.pad[1] + e.pad[3] + 2*e.borderW }
func (e *Element) padY() float32    { return e.pad[0] + e.pad[2] + 2*e.borderW }
func (e *Element) marginX() float32 { return e.margin[1] + e.margin[3] }
func (e *Element) marginY() float32 { return e.margin[0] + e.margin[2] }

func (e *Element) scrolls() bool { return e.flags&(flagScrollX|flagScrollY) != 0 }

// clampW and clampH apply the min and max sizes.
func (e *Element) clampW(w, cbW float32) float32 {
	if v, ok := e.maxW.resolve(base(cbW)); ok {
		w = min(w, v)
	}
	if v, ok := e.minW.resolve(base(cbW)); ok {
		w = max(w, v)
	}
	return max(w, 0)
}

func (e *Element) clampH(h, cbH float32) float32 {
	if v, ok := e.maxH.resolve(base(cbH)); ok {
		h = min(h, v)
	}
	if v, ok := e.minH.resolve(base(cbH)); ok {
		h = max(h, v)
	}
	return max(h, 0)
}

// base turns an unknown size into the -1 length.resolve takes for it.
func base(v float32) float32 {
	if finite(v) {
		return v
	}
	return -1
}

// layoutTree lays the frame out in a window of w×h DIPs and gives every
// element its box relative to the window.
func layoutTree(root *Element, w, h float32) {
	layoutBox(root, w, h)
	place(root, 0, 0)
}

// place turns the boxes, laid out relative to their parents, into window
// coordinates, moving the content of scroll containers by their offset.
func place(e *Element, x, y float32) {
	e.x += x
	e.y += y
	cx, cy := e.x, e.y
	if e.scrolls() {
		cx -= e.st.scrollX
		cy -= e.st.scrollY
	}
	for ch := e.first; ch != nil; ch = ch.next {
		if ch.flags&flagAbsolute != 0 {
			place(ch, e.x, e.y)
		} else {
			place(ch, cx, cy)
		}
	}
}

// textParams returns how to lay out the element's text at a content width
// (0 for one line per paragraph).
func (e *Element) textParams(width float32) text.Params {
	ts := e.resolvedText()
	p := text.Params{Text: e.text, Width: width, MaxLines: e.maxLines, Style: text.Style{
		Family: ts.family, Size: ts.size, Weight: ts.weight, Italic: ts.italic, LineHeight: ts.lineHeight,
		LetterSpacing: ts.spacing, Features: ts.features,
	}, Spans: e.textSpans()}
	switch ts.align {
	case Center:
		p.Align = text.Center
	case End:
		p.Align = text.End
	}
	return p
}

// resolvedText merges the text styles of the element and its ancestors.
func (e *Element) resolvedText() textStyle {
	var out textStyle
	for p := e; p != nil && out.set != setAll; p = p.parent {
		t := &p.ts
		take := t.set &^ out.set
		if take&setFamily != 0 {
			out.family = t.family
		}
		if take&setSize != 0 {
			out.size = t.size
		}
		if take&setWeight != 0 {
			out.weight = t.weight
		}
		if take&setItalic != 0 {
			out.italic = t.italic
		}
		if take&setColor != 0 {
			out.color = t.color
		}
		if take&setLineHeight != 0 {
			out.lineHeight = t.lineHeight
		}
		if take&setAlign != 0 {
			out.align = t.align
		}
		if take&setUnderline != 0 {
			out.underline = t.underline
		}
		if take&setSpacing != 0 {
			out.spacing = t.spacing
		}
		if take&setFeatures != 0 {
			out.features = t.features
		}
		if take&setStrike != 0 {
			out.strike = t.strike
		}
		out.set |= take
	}
	return out
}

// contentWidths returns the max-content and min-content widths of the
// element's own content (text, image, input), without padding.
func (e *Element) leafWidths() (maxW, minW float32) {
	switch e.kind {
	case kindText:
		if e.text == "" {
			return 0, 0
		}
		sys := textSystem()
		p := e.textParams(0)
		p.MaxLines = 0
		full := sys.Layout(p).Width
		if e.single {
			return full, 0
		}
		p.Width, p.NoBreakWords = 1, true
		return full, sys.Layout(p).Width
	case kindImage:
		w, _ := e.intrinsicSize()
		return w, 0
	case kindIcon:
		// Icons keep their size where room is short, as text does.
		w, _ := e.intrinsicSize()
		return w, w
	case kindInput:
		return 200, 0
	}
	return 0, 0
}

// intrinsic returns the element's max-content or min-content width, its
// border box.
func intrinsic(e *Element, maxContent bool) float32 {
	if v, ok := e.width.resolve(-1); ok {
		return e.clampW(v, inf)
	}
	var w float32
	if e.kind != kindBox {
		mx, mn := e.leafWidths()
		w = mn
		if maxContent {
			w = mx
		}
	} else {
		n := 0
		for ch := e.first; ch != nil; ch = ch.next {
			if ch.flags&flagAbsolute != 0 {
				continue
			}
			cw := intrinsic(ch, maxContent) + ch.marginX()
			if e.row && (maxContent || !e.wrap) {
				w += cw
				n++
			} else {
				w = max(w, cw)
			}
		}
		if e.row && n > 1 && (maxContent || !e.wrap) {
			w += e.gap * float32(n-1)
		}
		if !maxContent && e.scrolls() {
			w = 0
		}
	}
	return e.clampW(w+e.padX(), inf)
}

// fitWidth returns the width of an element sized by its content within
// avail DIPs, against a containing block cbW wide.
func fitWidth(e *Element, avail, cbW float32) float32 {
	if v, ok := e.width.resolve(base(cbW)); ok {
		return e.clampW(v, cbW)
	}
	if e.aspect > 0 {
		if h, ok := e.height.resolve(-1); ok {
			return e.clampW(h*e.aspect, cbW)
		}
	}
	mx := intrinsic(e, true)
	if !finite(avail) {
		return e.clampW(mx, cbW)
	}
	mn := intrinsic(e, false)
	return e.clampW(min(mx, max(mn, avail)), cbW)
}

// heightAt returns the height of the element when it is w wide, against
// a containing block cbH high.
func heightAt(e *Element, w, cbH float32) float32 {
	if v, ok := e.height.resolve(base(cbH)); ok {
		return e.clampH(v, cbH)
	}
	if e.aspect > 0 {
		return e.clampH(w/e.aspect, cbH)
	}
	for i := 0; i < e.nmeasure; i++ {
		if m := e.measures[i]; m.availW == w {
			return m.h
		}
	}
	h := contentHeight(e, w-e.padX()) + e.padY()
	h = e.clampH(h, cbH)
	if e.nmeasure < len(e.measures) {
		e.measures[e.nmeasure] = measure{availW: w, h: h}
		e.nmeasure++
	}
	return h
}

// contentHeight returns the height the content takes in a content box cw
// wide.
func contentHeight(e *Element, cw float32) float32 {
	switch e.kind {
	case kindText:
		return textSystem().Layout(e.textParams(max(cw, 1))).Height
	case kindImage, kindIcon:
		if w, h := e.intrinsicSize(); w > 0 {
			return cw * h / w
		}
		return 0
	case kindInput:
		return e.inputHeight()
	}
	if e.first == nil {
		return 0
	}
	_, h := flexLayout(e, cw, inf, false)
	return h
}

// layoutBox gives the element its size and lays out its content.
func layoutBox(e *Element, w, h float32) {
	e.w, e.h = w, h
	cw, ch := max(w-e.padX(), 0), max(h-e.padY(), 0)
	switch e.kind {
	case kindText:
		e.tl = textSystem().Layout(e.textParams(max(cw, 1)))
		if ed := e.st.editor; ed != nil && e.flags&flagSelectable != 0 {
			// Selectable text hit-tests and selects in what it shows.
			ed.layout = e.tl
			ed.originX, ed.originY = e.pad[3]+e.borderW, e.pad[0]+e.borderW
		}
		return
	case kindInput:
		e.layoutInput(cw, ch)
		return
	case kindImage, kindIcon:
		return
	}
	lw, lh := cw, ch
	if e.flags&flagScrollX != 0 {
		lw = inf
	}
	if e.flags&flagScrollY != 0 {
		lh = inf
	}
	uw, uh := flexLayout(e, lw, lh, true)
	if e.scrolls() {
		e.contentW = max(uw, cw) + e.padX()
		e.contentH = max(uh, ch) + e.padY()
	}
	layoutAbsolute(e)
}

type flexItem struct {
	e                    *Element
	base, hyp            float32
	minMain, maxMain     float32
	main, cross          float32
	frozen               bool
	marginMain, marginCr float32
	line                 int
}

type flexLine struct {
	start, end int
	main       float32 // sum of outer main sizes and gaps
	cross      float32
}

// flexScratch holds the items and lines of the flex containers being laid
// out, a nested container's after its parent's, reusing their memory
// frame after frame.
type flexScratch struct {
	items []flexItem
	lines []flexLine
}

// flexLayout lays out the in-flow children in a content box cw×ch (inf
// when unknown) and returns the size they take. With commit it gives them
// their boxes (relative to e) and lays them out in turn.
func flexLayout(e *Element, cw, ch float32, commit bool) (usedW, usedH float32) {
	row := e.row
	mainSize, crossSize := ch, cw
	if row {
		mainSize, crossSize = cw, ch
	}
	align := e.align
	if align == alignAuto {
		align = Stretch
	}
	scratch := &e.c.rt.flex
	firstItem, firstLine := len(scratch.items), len(scratch.lines)
	defer func() { scratch.items, scratch.lines = scratch.items[:firstItem], scratch.lines[:firstLine] }()
	for c := e.first; c != nil; c = c.next {
		if c.flags&flagAbsolute != 0 {
			continue
		}
		it := flexItem{e: c}
		if row {
			it.marginMain, it.marginCr = c.marginX(), c.marginY()
		} else {
			it.marginMain, it.marginCr = c.marginY(), c.marginX()
		}
		// The flex base size.
		if v, ok := c.basis.resolve(base(mainSize)); ok {
			it.base = v
		} else if row {
			if v, ok := c.width.resolve(base(cw)); ok {
				it.base = v
			} else {
				it.base = intrinsic(c, true)
			}
		} else {
			if v, ok := c.height.resolve(base(ch)); ok {
				it.base = v
			} else {
				it.base = heightAt(c, crossOf(c, cw, align), ch)
			}
		}
		// The automatic minimum is the content's, unless it scrolls.
		it.minMain, it.maxMain = 0, inf
		if row {
			if v, ok := c.minW.resolve(base(cw)); ok {
				it.minMain = v
			} else if !c.scrolls() && c.flags&flagClip == 0 {
				it.minMain = intrinsic(c, false)
				if v, ok := c.width.resolve(base(cw)); ok {
					it.minMain = min(it.minMain, v)
				}
			}
			if v, ok := c.maxW.resolve(base(cw)); ok {
				it.maxMain = v
			}
		} else {
			if v, ok := c.minH.resolve(base(ch)); ok {
				it.minMain = v
			} else if !c.scrolls() && c.flags&flagClip == 0 && c.grow == 0 {
				it.minMain = heightAt(c, crossOf(c, cw, align), inf)
				if v, ok := c.height.resolve(base(ch)); ok {
					it.minMain = min(it.minMain, v)
				}
			}
			if v, ok := c.maxH.resolve(base(ch)); ok {
				it.maxMain = v
			}
		}
		it.hyp = max(it.minMain, min(it.base, it.maxMain))
		// After the child's measures, which lay out containers in it.
		scratch.items = append(scratch.items, it)
	}
	items := scratch.items[firstItem:]

	// Break into lines.
	if len(items) > 0 {
		cur := flexLine{}
		for i := range items {
			outer := items[i].hyp + items[i].marginMain
			if e.wrap && finite(mainSize) && i > cur.start && cur.main+e.gap+outer > mainSize {
				cur.end = i
				scratch.lines = append(scratch.lines, cur)
				cur = flexLine{start: i}
			}
			if i > cur.start {
				cur.main += e.gap
			}
			cur.main += outer
			items[i].line = len(scratch.lines) - firstLine
		}
		cur.end = len(items)
		scratch.lines = append(scratch.lines, cur)
	}
	lines := scratch.lines[firstLine:]

	// Resolve the flexible lengths of each line.
	for li := range lines {
		ln := &lines[li]
		its := items[ln.start:ln.end]
		for i := range its {
			its[i].main = its[i].hyp
		}
		if !finite(mainSize) {
			continue
		}
		resolveFlexible(its, mainSize-e.gap*float32(len(its)-1), ln.main-e.gap*float32(len(its)-1) < mainSize-e.gap*float32(len(its)-1))
		ln.main = e.gap * float32(len(its)-1)
		for _, it := range its {
			ln.main += it.main + it.marginMain
		}
	}

	// Cross sizes: natural ones first, then stretched ones fill the line.
	for li := range lines {
		ln := &lines[li]
		for i := ln.start; i < ln.end; i++ {
			it := &items[i]
			c := it.e
			if row {
				it.cross = heightAt(c, it.main, ch)
			} else {
				if v, ok := c.width.resolve(base(cw)); ok {
					it.cross = c.clampW(v, cw)
				} else if stretches(c, align) && finite(cw) {
					it.cross = c.clampW(cw-it.marginCr, cw)
				} else {
					it.cross = fitWidth(c, cw-it.marginCr, cw)
				}
			}
			ln.cross = max(ln.cross, it.cross+it.marginCr)
		}
	}
	if len(lines) == 1 && finite(crossSize) && !e.wrap {
		lines[0].cross = crossSize
	}
	for li := range lines {
		ln := &lines[li]
		for i := ln.start; i < ln.end; i++ {
			it := &items[i]
			c := it.e
			if !stretches(c, align) {
				continue
			}
			if row {
				if _, ok := c.height.resolve(base(ch)); !ok && c.aspect == 0 {
					it.cross = c.clampH(ln.cross-it.marginCr, ch)
				}
			} else if _, ok := c.width.resolve(base(cw)); !ok {
				it.cross = c.clampW(ln.cross-it.marginCr, cw)
			}
		}
	}

	// The size the lines take.
	var usedMain, usedCross float32
	for li, ln := range lines {
		usedMain = max(usedMain, ln.main)
		usedCross += ln.cross
		if li > 0 {
			usedCross += e.gap
		}
	}
	if row {
		usedW, usedH = usedMain, usedCross
	} else {
		usedW, usedH = usedCross, usedMain
	}
	if !commit {
		return usedW, usedH
	}

	// Place the items.
	contentMain := mainSize
	if !finite(contentMain) {
		contentMain = usedMain
	}
	crossPos := float32(0)
	for _, ln := range lines {
		its := items[ln.start:ln.end]
		free := max(contentMain-ln.main, 0)
		pos, between := justifyOffsets(e.justify, free, len(its))
		for i := range its {
			it := &its[i]
			c := it.e
			var crossOff float32
			switch selfAlign(c, align) {
			case Center:
				crossOff = (ln.cross - it.cross - it.marginCr) / 2
			case End:
				crossOff = ln.cross - it.cross - it.marginCr
			}
			if row {
				c.x = e.pad[3] + e.borderW + pos + c.margin[3]
				c.y = e.pad[0] + e.borderW + crossPos + crossOff + c.margin[0]
				layoutBox(c, it.main, it.cross)
			} else {
				c.x = e.pad[3] + e.borderW + crossPos + crossOff + c.margin[3]
				c.y = e.pad[0] + e.borderW + pos + c.margin[0]
				layoutBox(c, it.cross, it.main)
			}
			pos += it.main + it.marginMain + e.gap + between
		}
		crossPos += ln.cross + e.gap
	}
	return usedW, usedH
}

// crossOf returns the width a column child gets: stretched to the column
// or fitting its content.
func crossOf(c *Element, cw float32, align Align) float32 {
	if v, ok := c.width.resolve(base(cw)); ok {
		return c.clampW(v, cw)
	}
	if stretches(c, align) && finite(cw) {
		return c.clampW(cw-c.marginX(), cw)
	}
	return fitWidth(c, cw-c.marginX(), cw)
}

// stretches reports whether c stretches across its line, as aligning by
// align asks, which icons do not: they keep their size.
func stretches(c *Element, align Align) bool {
	return selfAlign(c, align) == Stretch && c.kind != kindIcon
}

func selfAlign(c *Element, align Align) Align {
	if c.self != alignAuto {
		return c.self
	}
	return align
}

// resolveFlexible grows or shrinks the items of a line to fill space,
// honoring their minimum and maximum sizes (CSS flexbox §9.7).
func resolveFlexible(its []flexItem, space float32, growing bool) {
	for i := range its {
		it := &its[i]
		it.frozen = false
		factor := it.e.grow
		if !growing {
			factor = it.e.shrink * it.base
		}
		if factor == 0 || (growing && it.base > it.hyp) || (!growing && it.base < it.hyp) {
			it.main, it.frozen = it.hyp, true
		}
	}
	for range its {
		free := space
		var sum float32
		for _, it := range its {
			if it.frozen {
				free -= it.main + it.marginMain
			} else {
				free -= it.base + it.marginMain
				if growing {
					sum += it.e.grow
				} else {
					sum += it.e.shrink * it.base
				}
			}
		}
		if sum == 0 {
			break
		}
		var violation float32
		for i := range its {
			it := &its[i]
			if it.frozen {
				continue
			}
			if growing {
				it.main = it.base + free*it.e.grow/sum
			} else {
				it.main = it.base + free*it.e.shrink*it.base/sum
			}
			clamped := max(it.minMain, min(it.main, it.maxMain))
			violation += clamped - it.main
			it.main = clamped
		}
		if violation == 0 {
			break
		}
		for i := range its {
			it := &its[i]
			if it.frozen {
				continue
			}
			if (violation > 0 && it.main == it.minMain) || (violation < 0 && it.main == it.maxMain) {
				it.frozen = true
			}
		}
	}
}

// justifyOffsets returns where the first item goes and the extra space
// between items.
func justifyOffsets(j Align, free float32, n int) (start, between float32) {
	switch j {
	case Center:
		return free / 2, 0
	case End:
		return free, 0
	case SpaceBetween:
		if n > 1 {
			return 0, free / float32(n-1)
		}
	case SpaceAround:
		if n > 0 {
			return free / float32(n) / 2, free / float32(n)
		}
	case SpaceEvenly:
		return free / float32(n+1), free / float32(n+1)
	}
	return 0, 0
}

// layoutAbsolute places the absolute children in the padding box.
func layoutAbsolute(e *Element) {
	pw, ph := e.w-2*e.borderW, e.h-2*e.borderW
	for c := e.first; c != nil; c = c.next {
		if c.flags&flagAbsolute == 0 {
			continue
		}
		top, tok := c.inset[0].resolve(ph)
		right, rok := c.inset[1].resolve(pw)
		bottom, bok := c.inset[2].resolve(ph)
		left, lok := c.inset[3].resolve(pw)
		var w float32
		if v, ok := c.width.resolve(pw); ok {
			w = c.clampW(v, pw)
		} else if lok && rok {
			w = c.clampW(pw-left-right-c.marginX(), pw)
		} else {
			w = fitWidth(c, pw-c.marginX(), pw)
		}
		var h float32
		if v, ok := c.height.resolve(ph); ok {
			h = c.clampH(v, ph)
		} else if tok && bok {
			h = c.clampH(ph-top-bottom-c.marginY(), ph)
		} else {
			h = heightAt(c, w, ph)
		}
		if c.place.on {
			left, top = c.place.fit(c, left, top, w, h, pw, ph)
		}
		x := left + c.margin[3]
		if !lok && rok {
			x = pw - right - w - c.margin[1]
		}
		y := top + c.margin[0]
		if !tok && bok {
			y = ph - bottom - h - c.margin[2]
		}
		c.x, c.y = e.borderW+x, e.borderW+y
		layoutBox(c, w, h)
	}
}
