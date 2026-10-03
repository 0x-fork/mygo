package ui

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/text"
)

// Span is a run of a RichText with a style of its own. What it leaves
// zero takes the style of the text around it, and Italic, Underline and
// Strikethrough only turn those on.
type Span struct {
	Text          string
	Font          string // a family, as for Element.Font
	Size          float32
	Weight        int
	Italic        bool
	Color         Color
	Underline     bool
	Strikethrough bool
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
// a frame.
func (e *Element) textSpans() string {
	if e.spans == nil || e.spansKey != "" {
		return e.spansKey
	}
	ts := make([]text.Span, len(e.spans))
	end := 0
	for i, s := range e.spans {
		end += utf8.RuneCountInString(s.Text)
		ts[i] = text.Span{End: end, Family: s.Font, Size: s.Size, Weight: s.Weight, Italic: s.Italic, LetterSpacing: s.LetterSpacing, Features: s.Features}
	}
	e.spansKey = text.EncodeSpans(ts)
	return e.spansKey
}

// spanPaint paints the colors and lines of a rich text's spans: ends are
// the runes ending each span.
type spanPaint struct {
	spans []Span
	ends  []int
}

func newSpanPaint(spans []Span) *spanPaint {
	for _, s := range spans {
		if s.Color.A > 0 || s.Underline || s.Strikethrough {
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

// lines draws the underlines and strikethroughs of the spans of a line,
// from the left of the text at x, in DIPs, and its baseline in device
// pixels.
func (sp *spanPaint) lines(p *Painter, line *text.Line, x, baseline float32, color Color) {
	s := p.scale
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
		if k < 0 || !sp.spans[k].Underline && !sp.spans[k].Strikethrough {
			continue
		}
		c := sp.color(k, color).scene()
		thick := max(round(size*s/14), 1)
		rect := func(y float32) {
			p.s.Ops = append(p.s.Ops, scene.Op{Kind: scene.OpFill, Rect: scene.Rect{X: round((x + x0) * s), Y: y, W: round((x1 - x0) * s), H: thick}, Color: c, Opacity: p.opacity})
		}
		if sp.spans[k].Underline {
			rect(baseline + max(round(size*s/10), 1))
		}
		if sp.spans[k].Strikethrough {
			rect(baseline - round(size*s*0.27))
		}
	}
}
