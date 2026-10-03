// Package scene is the display list the ui package paints a frame into and
// the renderers draw: rounded rectangles with borders and gradients, box
// shadows, glyphs from a shared atlas, images and clips, in paint order.
// Geometry is in device pixels with the origin at the top-left corner.
package scene

import (
	"image"
	"image/draw"
	"sync/atomic"
)

// Color is a straight (not premultiplied) sRGB color.
type Color struct{ R, G, B, A uint8 }

// Premul returns the color premultiplied by its alpha, with every component
// between 0 and 1, times opacity.
func (c Color) Premul(opacity float32) [4]float32 {
	a := float32(c.A) / 255 * opacity
	return [4]float32{float32(c.R) / 255 * a, float32(c.G) / 255 * a, float32(c.B) / 255 * a, a}
}

// Rect is a rectangle in device pixels.
type Rect struct{ X, Y, W, H float32 }

// Empty reports whether the rectangle has no area.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Intersect returns the intersection of r and o.
func (r Rect) Intersect(o Rect) Rect {
	x0, y0 := max(r.X, o.X), max(r.Y, o.Y)
	x1, y1 := min(r.X+r.W, o.X+o.W), min(r.Y+r.H, o.Y+o.H)
	if x1 <= x0 || y1 <= y0 {
		return Rect{X: x0, Y: y0}
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// Contains reports whether the point is inside r.
func (r Rect) Contains(x, y float32) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H
}

// Kind is the kind of an Op.
type Kind uint8

const (
	// OpFill paints a rounded rectangle with Color, or the Paint of Color
	// and Color2, and, where Border is positive, a border of BorderColor
	// inside its edges, dashed or not.
	OpFill Kind = iota
	// OpShadow paints the blurred shadow of a rounded rectangle: Rect and
	// Radii are the shadow's box, already offset and spread, Blur its blur
	// radius and Color its color.
	OpShadow
	// OpGlyphs paints Scene.Glyphs[Start:End]. With a gradient Paint, its
	// mask glyphs take the gradient from Color to Color2, times Opacity,
	// instead of their own colors.
	OpGlyphs
	// OpImage paints Src of Image into Rect, clipped to Radii, with
	// Opacity, in shades of gray if Grayscale.
	OpImage
	// OpPushClip clips the following ops to Rect with Radii, intersected
	// with the clip in effect, until the matching OpPopClip.
	OpPushClip
	// OpPopClip restores the clip in effect before the matching OpPushClip.
	OpPopClip
)

// Op is one drawing operation. Which fields matter depends on Kind.
type Op struct {
	Kind Kind
	Rect Rect
	// Radii are the corner radii: top-left, top-right, bottom-right and
	// bottom-left.
	Radii [4]float32

	Color Color
	// Paint, unless PaintSolid, fills with Color and Color2 as Gradient
	// says.
	Paint    Paint
	Color2   Color
	Gradient [4]float32

	// Border holds the widths of the border on the top, right, bottom and
	// left; Dashed dashes it.
	Border      [4]float32
	BorderColor Color
	Dashed      bool

	Blur float32

	Start, End int32

	Image     *Image
	Src       Rect
	Grayscale bool
	// Opacity multiplies the op's colors; 0 means 1.
	Opacity float32
}

// Paint is what fills an OpFill besides plain Color, or the masks of an
// OpGlyphs (gradients only).
type Paint uint8

const (
	PaintSolid Paint = iota
	// PaintLinear is a linear gradient from Color at (Gradient[0],
	// Gradient[1]) to Color2 at (Gradient[2], Gradient[3]), mixed in sRGB;
	// PaintOklab mixes them in Oklab. Both mix premultiplied colors, as
	// CSS does.
	PaintLinear
	PaintOklab
	// PaintStripes draws stripes of Color over Color2: Gradient holds the
	// unit vector across the stripes, their width and their period, from
	// the top-left corner of Rect.
	PaintStripes
)

// Uniform returns the widths of a border as wide on every side.
func Uniform(w float32) [4]float32 { return [4]float32{w, w, w, w} }

// HasBorder reports whether any of the widths is positive.
func HasBorder(w [4]float32) bool { return w[0] > 0 || w[1] > 0 || w[2] > 0 || w[3] > 0 }

// InnerRadii returns the radii of the inner edge of a border of widths w
// (top, right, bottom, left) inside a rounded rectangle r with radii,
// which FitRadii already fitted: each corner's less the wider of its two
// sides, fitted to the inner rectangle.
func InnerRadii(r Rect, radii, w [4]float32) (Rect, [4]float32) {
	inner := Rect{X: r.X + w[3], Y: r.Y + w[0], W: r.W - w[1] - w[3], H: r.H - w[0] - w[2]}
	var out [4]float32
	for i, side := range [4][2]int{{0, 3}, {0, 1}, {2, 1}, {2, 3}} {
		out[i] = max(radii[i]-max(w[side[0]], w[side[1]]), 0)
	}
	return inner, FitRadii(inner, out)
}

// Glyph is a glyph mask or color glyph copied from an atlas into a frame.
type Glyph struct {
	// X, Y, W and H place the glyph's bitmap, in device pixels.
	X, Y, W, H float32
	// U, V, UW and VH are the bitmap's rectangle in its atlas.
	U, V, UW, VH uint16
	// Color tints mask glyphs; color glyphs take its alpha only.
	Color Color
	// Colored glyphs come from Scene.ColorAtlas, the others from
	// Scene.MaskAtlas.
	Colored bool
}

// Scene is a frame's display list.
type Scene struct {
	// Width and Height of the frame in device pixels; Scale is how many
	// device pixels a DIP is (0 for 1).
	Width, Height int
	Scale         float32
	// Clear is the color the frame starts with.
	Clear  Color
	Ops    []Op
	Glyphs []Glyph
	// MaskAtlas holds coverage masks (one byte per pixel), ColorAtlas
	// premultiplied RGBA bitmaps such as emoji.
	MaskAtlas, ColorAtlas *Atlas
}

// Reset empties the scene for another frame, keeping its storage.
func (s *Scene) Reset(width, height int, clear Color) {
	s.Width, s.Height, s.Clear = width, height, clear
	s.Ops = s.Ops[:0]
	s.Glyphs = s.Glyphs[:0]
}

var lastImageID atomic.Uint64

// Image is a bitmap a scene draws. Renderers keep a texture per image and
// upload it again when its version changes.
type Image struct {
	id      uint64
	version uint64
	// W and H are the size in pixels; Pix holds premultiplied RGBA rows of
	// 4*W bytes.
	W, H int
	Pix  []byte
}

// NewImage converts img to a premultiplied RGBA bitmap.
func NewImage(img image.Image) *Image {
	b := img.Bounds()
	rgba, ok := img.(*image.RGBA)
	if !ok || rgba.Stride != 4*b.Dx() || b.Min != (image.Point{}) {
		rgba = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	}
	return &Image{id: lastImageID.Add(1), version: 1, W: b.Dx(), H: b.Dy(), Pix: rgba.Pix}
}

// NewImageRGBA wraps premultiplied RGBA pixels of a w×h bitmap.
func NewImageRGBA(w, h int, pix []byte) *Image {
	return &Image{id: lastImageID.Add(1), version: 1, W: w, H: h, Pix: pix}
}

// ID identifies the image for texture caches.
func (m *Image) ID() uint64 { return m.id }

// Version changes whenever the pixels change.
func (m *Image) Version() uint64 { return m.version }

// Changed records that Pix was modified.
func (m *Image) Changed() { m.version++ }

// FitRadii scales corner radii down so that adjacent ones fit along each
// side of r, as CSS does; renderers apply it before drawing.
func FitRadii(r Rect, radii [4]float32) [4]float32 {
	f := float32(1)
	if s := radii[0] + radii[1]; s > r.W && s > 0 {
		f = min(f, r.W/s)
	}
	if s := radii[3] + radii[2]; s > r.W && s > 0 {
		f = min(f, r.W/s)
	}
	if s := radii[0] + radii[3]; s > r.H && s > 0 {
		f = min(f, r.H/s)
	}
	if s := radii[1] + radii[2]; s > r.H && s > 0 {
		f = min(f, r.H/s)
	}
	for i := range radii {
		radii[i] = max(radii[i]*f, 0)
	}
	return radii
}
