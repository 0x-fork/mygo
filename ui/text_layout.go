package ui

import (
	"slices"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/egoist/mygo/internal/text"
)

// TextLayout is retained shaped text for a custom text control. It owns font
// and geometry results, not editable document state. Cache layouts of the
// paragraphs or lines you display, and replace only those affected by an edit.
// Its geometry methods are safe from any goroutine. Drawing uses a Painter
// during the view's frame. Offsets are UTF-16, matching TextInputClient.
type TextLayout struct {
	mu         sync.Mutex
	l          *text.Layout
	units      []int // rune offset -> UTF-16 offset
	boundaries []int // grapheme boundaries in runes
	spans      []Span
}

// ShapeText shapes text with the system's fonts, fallback, bidi and line
// breaking, wrapping at width DIPs (zero only breaks at newlines). It does not
// retain a copy of an editor document or install keyboard/editing behavior.
func ShapeText(s string, font Font, width float32) *TextLayout { return shapeText(s, font, width, nil) }

// ShapeRichText shapes the existing display-only Span styles into retained
// geometry, so an application-owned rich editor can render its own runs.
func ShapeRichText(spans []Span, font Font, width float32) *TextLayout {
	var b strings.Builder
	for _, sp := range spans {
		b.WriteString(sp.Text)
	}
	return shapeText(b.String(), font, width, slices.Clone(spans))
}

func shapeText(s string, font Font, width float32, spans []Span) *TextLayout {
	l := text.Shared().Shape(text.Params{Text: s, Style: font.style(), Width: width, KeepSpaces: true, Spans: encodeSpans(spans)})
	t := &TextLayout{l: l, units: make([]int, len(l.Runes)+1), spans: spans}
	for i, r := range l.Runes {
		t.units[i+1] = t.units[i] + utf16.RuneLen(r)
	}
	var boundaries text.Boundaries
	boundaries.Reset(l.Runes)
	// Boundary iteration is per shaped paragraph, never per document edit.
	t.boundaries = boundaries.GraphemeOffsets()
	return t
}

// Size is the layout's extent in DIPs.
func (t *TextLayout) Size() (float32, float32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.l.Width, t.l.Height
}

func (t *TextLayout) runeAt(index int, forward bool) int {
	i, found := slices.BinarySearch(t.units, max(0, min(index, t.units[len(t.units)-1])))
	if !found && !forward {
		i--
	}
	b, found := slices.BinarySearch(t.boundaries, i)
	if !found {
		if forward {
			i = t.boundaries[b]
		} else {
			i = t.boundaries[b-1]
		}
	}
	return i
}

// Caret gives the caret rectangle at index, snapped to a whole grapheme.
func (t *TextLayout) Caret(index int) Rect {
	t.mu.Lock()
	defer t.mu.Unlock()
	x, y, h := t.l.Caret(t.runeAt(index, false))
	return Rect{x, y, 1, h}
}

// PreviousBoundary and NextBoundary find neighboring whole-grapheme offsets
// for logical deletion and navigation. Visual selection policy remains with
// the application, which can keep multiple ranges and caret affinity.
func (t *TextLayout) PreviousBoundary(index int) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	i := t.runeAt(index, false)
	b, _ := slices.BinarySearch(t.boundaries, i)
	return t.units[t.boundaries[max(0, b-1)]]
}
func (t *TextLayout) NextBoundary(index int) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	i := t.runeAt(index, false)
	b, _ := slices.BinarySearch(t.boundaries, i)
	return t.units[t.boundaries[min(len(t.boundaries)-1, b+1)]]
}

// IndexAt finds the nearest whole-grapheme caret in visual layout order.
func (t *TextLayout) IndexAt(point Point) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	i := t.l.IndexAt(point.X, point.Y)
	lo, hi := t.runeAt(t.units[i], false), t.runeAt(t.units[i], true)
	if lo != hi {
		distance := func(index int) float32 {
			x, y, h := t.l.Caret(index)
			dx, dy := x-point.X, y+h/2-point.Y
			return dx*dx + dy*dy
		}
		if distance(hi) < distance(lo) {
			lo = hi
		}
	}
	return t.units[lo]
}

// SelectionRects returns the visual rectangles of one or more logical
// ranges, preserving gaps in bidi selections. An editor supplies its own
// range set and selection policy; the layout does not reduce it to one range.
func (t *TextLayout) SelectionRects(ranges ...TextInputRange) []Rect {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []Rect
	for _, r := range ranges {
		r = platformTextRange(r)
		for _, b := range t.l.SelectionVisual(t.runeAt(r.Start, false), t.runeAt(r.End, true), false) {
			out = append(out, Rect{b.X, b.Y, b.W, b.H})
		}
	}
	return out
}

func platformTextRange(r TextInputRange) TextInputRange {
	if r.Start > r.End {
		r.Start, r.End = r.End, r.Start
	}
	return r
}

// TextLayout paints a retained layout at (x,y) in DIPs, with color as the
// default foreground. Span colors and decorations paint as RichText does.
func (p *Painter) TextLayout(layout *TextLayout, x, y float32, color Color) {
	if layout == nil {
		return
	}
	layout.mu.Lock()
	defer layout.mu.Unlock()
	p.textLayout(layout.l, x, y, color, textStyle{size: layout.l.Params.Style.FontSize()}, newSpanPaint(layout.spans))
}
