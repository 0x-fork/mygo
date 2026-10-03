package ui

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/text"
)

// Span is a run of a RichText with a style of its own. What it leaves
// zero takes the style of the text around it, and Italic, Underline,
// WavyUnderline and Strikethrough only turn those on.
type Span struct {
	Text          string
	Font          string // a family, as for Element.Font
	Size          float32
	Weight        int
	Italic        bool
	Color         Color
	Underline     bool
	WavyUnderline bool
	Strikethrough bool
	// DecorationColor and DecorationThickness are those of the span's
	// underline and strikethrough, as for Element.DecorationColor.
	DecorationColor     Color
	DecorationThickness float32
	// Background highlights the span, as search results are.
	Background    Color
	LetterSpacing float32
	Features      string // as for Element.FontFeatures, comma separated
}

// RichText creates a text whose spans differ in style, over the style of
// the text, which its methods and the elements around it set as for Text:
//
//	ui.RichText(c,
//		ui.Span{Text: "Saved "},
//		ui.Span{Text: "report.pdf", Weight: 600},
//		ui.Span{Text: " to "},
//		ui.Span{Text: "Documents", Color: t.Accent, Underline: true},
//	)
//
// The spans wrap together as one paragraph, or several at newlines.
//
// Text elements built in its Children (Text, Link, RichText) continue the
// paragraph after its spans, as HTML's inline elements do: each styles its
// own text over the paragraph's, and keeps its interaction (Clicked,
// Hovered, the focus, a Tooltip, assistive technology) with its words as
// its area. A link in a sentence:
//
//	ui.RichText(c).Children(func() {
//		ui.Text(c, "Read ")
//		ui.Link(c, "the guide", url)
//		ui.Text(c, " to get started.")
//	})
//
// Only text elements go inside a text, and their sizes, padding, borders
// and corners do not apply; a background highlights their text.
func RichText(c *Context, spans ...Span) *Element {
	e := c.newElement(kindText)
	n := 0
	for _, s := range spans {
		n += len(s.Text)
	}
	var b strings.Builder
	b.Grow(n)
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	e.text = b.String()
	e.spans = spans
	return e
}

// textSpans encodes the styles of an element's spans for its layout, once
// a frame, with those of the elements inside it, once it is built.
func (e *Element) textSpans() string {
	if e.first != nil && !e.merged {
		e.spans, e.merged = e.inlineSpans(), true
	}
	if e.spans == nil || e.spansKey != "" {
		return e.spansKey
	}
	e.spansKey = encodeSpans(e.spans)
	return e.spansKey
}

// encodeSpans encodes the styles of spans for a layout.
func encodeSpans(spans []Span) string {
	ts := make([]text.Span, len(spans))
	end := 0
	for i, s := range spans {
		end += utf8.RuneCountInString(s.Text)
		ts[i] = text.Span{End: end, Family: s.Font, Size: s.Size, Weight: s.Weight, Italic: s.Italic, LetterSpacing: s.LetterSpacing, Features: s.Features}
	}
	return text.EncodeSpans(ts)
}

// richParams returns how to lay out spans over the theme's text style,
// wrapping lines at width (none for 0).
func (rt *engine) richParams(width float32, spans []Span) text.Params {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	t := rt.c.theme
	return text.Params{Text: b.String(), Width: width, Style: text.Style{Family: t.Font, Size: t.FontSize}, Spans: encodeSpans(spans)}
}

// RichText draws spans of text as RichText shows them, with its top-left
// corner at (x, y), wrapping lines at width DIPs (none for 0), and returns
// the size it takes. What the spans leave zero takes the theme's font,
// FontSize and Text color:
//
//	w, _ := p.MeasureText(0, label)
//	p.RichText(r.X+(r.W-w)/2, r.Y, 0, label)
func (p *Painter) RichText(x, y, width float32, spans ...Span) (w, h float32) {
	l := p.rt.text.Layout(p.rt.richParams(width, spans))
	t := p.rt.c.theme
	p.textLayout(l, x, y, t.Text, textStyle{size: t.FontSize}, newSpanPaint(spans))
	return l.Width, l.Height
}

// MeasureText returns the size spans of text take as Painter.RichText draws
// them, wrapping lines at width DIPs (none for 0).
func (p *Painter) MeasureText(width float32, spans ...Span) (w, h float32) {
	l := p.rt.text.Layout(p.rt.richParams(width, spans))
	return l.Width, l.Height
}

// MeasureText returns the size spans of text take as Painter.RichText draws
// them, wrapping lines at width DIPs (none for 0), to lay out what depends
// on it while building.
func (c *Context) MeasureText(width float32, spans ...Span) (w, h float32) {
	l := c.rt.text.Layout(c.rt.richParams(width, spans))
	return l.Width, l.Height
}

// spanPaint paints the colors and lines of a rich text's spans: ends are
// the runes ending each span.
type spanPaint struct {
	spans []Span
	ends  []int
}

func newSpanPaint(spans []Span) *spanPaint {
	for _, s := range spans {
		if s.Color.A > 0 || s.Underline || s.WavyUnderline || s.Strikethrough || s.Background.A > 0 {
			sp := &spanPaint{spans: spans, ends: make([]int, len(spans))}
			end := 0
			for i, s := range spans {
				end += utf8.RuneCountInString(s.Text)
				sp.ends[i] = end
			}
			return sp
		}
	}
	return nil
}

// at returns the index of the span holding rune r, or -1.
func (sp *spanPaint) at(r int) int {
	lo, hi := 0, len(sp.ends)
	for lo < hi {
		m := (lo + hi) / 2
		if sp.ends[m] <= r {
			lo = m + 1
		} else {
			hi = m
		}
	}
	if lo == len(sp.ends) {
		return -1
	}
	return lo
}

// color returns the color of span i, or c.
func (sp *spanPaint) color(i int, c Color) Color {
	if i >= 0 && sp.spans[i].Color.A > 0 {
		return sp.spans[i].Color
	}
	return c
}

// runs calls fn with each run of the glyphs of a line in one span: the
// span's index (-1 past the spans), and the run's left, right and largest
// size, in DIPs from the left of the text.
func (sp *spanPaint) runs(line *text.Line, fn func(k int, x0, x1, size float32)) {
	gs := line.Glyphs
	for i := 0; i < len(gs); {
		k := sp.at(gs[i].Cluster)
		x0, x1, size := float32(math.MaxFloat32), float32(-math.MaxFloat32), float32(0)
		j := i
		for ; j < len(gs) && sp.at(gs[j].Cluster) == k; j++ {
			x0, x1 = min(x0, gs[j].X), max(x1, gs[j].X+gs[j].Advance)
			size = max(size, gs[j].Size)
		}
		i = j
		fn(k, x0, x1, size)
	}
}

// backgrounds fills behind the spans of a line that have a background,
// from the top-left of the text at (x, y), in DIPs.
func (sp *spanPaint) backgrounds(p *Painter, line *text.Line, x, y float32) {
	sp.runs(line, func(k int, x0, x1, _ float32) {
		if k >= 0 && sp.spans[k].Background.A > 0 {
			p.Fill(Rect{x + x0, y + line.Y, x1 - x0, line.Height}, sp.spans[k].Background, 0)
		}
	})
}

// lines draws the underlines and strikethroughs of the spans of a line,
// from the left of the text at x, in DIPs, and its baseline in device
// pixels, with the color and thickness of base where the spans set none.
func (sp *spanPaint) lines(p *Painter, line *text.Line, x, baseline float32, color Color, base decoration) {
	s := p.scale
	sp.runs(line, func(k int, x0, x1, size float32) {
		if k < 0 {
			return
		}
		span := &sp.spans[k]
		d := decoration{underline: span.Underline || span.WavyUnderline, wavy: span.WavyUnderline, strike: span.Strikethrough, color: base.color, thick: base.thick}
		if !d.underline && !d.strike {
			return
		}
		if span.DecorationColor.A > 0 {
			d.color = span.DecorationColor
		}
		if span.DecorationThickness > 0 {
			d.thick = span.DecorationThickness
		}
		p.decorate((x+x0)*s, (x+x1)*s, baseline, baseline-round(size*s*0.27), size, d, sp.color(k, color))
	})
}
