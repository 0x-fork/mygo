// Package gpu turns scenes into what the GPU renderers draw: one instanced
// quad per operation, for a shader that computes rounded rectangles,
// borders, gradients, stripes, shadows and atlas coverage itself, in
// batches that share a scissor rectangle and an image. Every renderer
// (d3d11, metal, gl) draws the same instances, the way internal/raster
// draws scenes on the CPU.
//
// The shaders write a second color for blending, the source's alpha for
// each channel: the alpha of the color for everything but subpixel
// glyphs, whose subpixels cover each channel by its own. Renderers blend
// with dual-source blending: the destination times one minus the second
// color, plus the first.
package gpu

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strings"
	"unsafe"

	"github.com/egoist/mygo/internal/scene"
)

// SourceSum returns the SHA-256, in hex, of a shader's source as the
// repository holds it, with LF line endings, which shaders compiled ahead
// of time record: the source matches them however it was checked out, as
// with CRLF endings on Windows.
func SourceSum(src string) string {
	sum := sha256.Sum256([]byte(strings.ReplaceAll(src, "\r\n", "\n")))
	return hex.EncodeToString(sum[:])
}

// Instance is the data of one quad, in device pixels, as the shaders read
// it: eleven float4s.
type Instance struct {
	// Rect is x, y, width, height; Radii the corners' radii (top-left,
	// top-right, bottom-right, bottom-left), negative for continuous
	// corners (scene.Corners), which only Metal's shader draws, or a
	// glyph's gamma ratios
	// (scene.TextParams); Inner those of a border's inner edge, or of the
	// box casting a shadow, or a glyph's contrast and thin boost in Inner[0]
	// and Inner[1].
	Rect, Radii, Inner [4]float32
	// Color and Color2 (a gradient's end) and Border are straight RGBA.
	Color, Color2, Border [4]float32
	// Grad holds a gradient's start and end points, or the stripes' unit
	// vector across them, width and period; UV the texture rectangle,
	// normalized, a fill's border widths (top, right, bottom, left), or
	// the box casting a shadow, which shows only outside it (none when
	// empty).
	Grad, UV [4]float32
	// Clip and ClipRadii are the innermost clip, which the shader cuts.
	Clip, ClipRadii [4]float32
	// Params is the kind (0 fill, 1 shadow, 2 mask glyph, 3 color glyph, 4
	// image, 5 subpixel glyph); 1 for a dashed border or a grayscale image;
	// the shadow's sigma (0 for none) or the paint (scene.Paint); and the
	// opacity.
	Params [4]float32
}

// InstanceSize is the size of an Instance in bytes.
const InstanceSize = int(unsafe.Sizeof(Instance{}))

// Scissor is a scissor rectangle in device pixels, the layout of a
// Direct3D RECT.
type Scissor struct{ Left, Top, Right, Bottom int32 }

// Empty reports whether the rectangle has no pixels.
func (s Scissor) Empty() bool { return s.Right <= s.Left || s.Bottom <= s.Top }

// Batch is a run of instances that draw with the same scissor rectangle
// and image.
type Batch struct {
	Start, Count int
	Scissor      Scissor
	// Image is the renderer's texture of the batch's image, or 0.
	Image uintptr
}

// Builder builds the instances and batches of scenes, reusing its memory
// from frame to frame.
type Builder struct {
	Instances []Instance
	Batches   []Batch
	stack     []clip
}

type clip struct {
	rect    scene.Rect
	radii   [4]float32
	bounds  scene.Rect
	scissor Scissor
}

// Build turns the operations of s into Instances and Batches. image
// returns the renderer's texture of an image, uploading it when new or
// changed, or 0 to leave the image out.
func (b *Builder) Build(s *scene.Scene, image func(*scene.Image) uintptr) {
	b.Instances = b.Instances[:0]
	b.Batches = b.Batches[:0]
	everything := scene.Rect{X: -1e6, Y: -1e6, W: 2e6, H: 2e6}
	b.stack = append(b.stack[:0], clip{rect: everything, bounds: scene.Rect{W: float32(s.Width), H: float32(s.Height)},
		scissor: Scissor{0, 0, int32(s.Width), int32(s.Height)}})
	for i := range s.Ops {
		op := &s.Ops[i]
		switch op.Kind {
		case scene.OpPushClip:
			top := b.stack[len(b.stack)-1]
			bounds := top.bounds.Intersect(op.Rect)
			// The innermost clip shapes its edges in the shader, the
			// others cut with the scissor rectangle.
			c := clip{rect: op.Rect, radii: scene.Corners(op.Rect, op.Radii, op.Continuous), bounds: bounds}
			if bounds.W > 0 && bounds.H > 0 {
				c.scissor = Scissor{int32(math.Floor(float64(bounds.X))), int32(math.Floor(float64(bounds.Y))),
					int32(math.Ceil(float64(bounds.X + bounds.W))), int32(math.Ceil(float64(bounds.Y + bounds.H)))}
			}
			b.stack = append(b.stack, c)
		case scene.OpPopClip:
			if len(b.stack) > 1 {
				b.stack = b.stack[:len(b.stack)-1]
			}
		case scene.OpFill:
			if op.Rect.Empty() {
				continue
			}
			bw := op.Border
			if op.BorderColor.A == 0 {
				bw = [4]float32{}
			}
			radii := scene.Corners(op.Rect, op.Radii, op.Continuous)
			var inner [4]float32
			if scene.HasBorder(bw) {
				_, inner = scene.InnerRadii(op.Rect, radii, bw)
			}
			dashed := float32(0)
			if op.Dashed {
				dashed = 1
			}
			b.add(Instance{
				Rect: rect(op.Rect), Radii: radii, Inner: inner,
				Color: straight(op.Color), Color2: straight(op.Color2), Border: straight(op.BorderColor), Grad: op.Gradient,
				UV:     bw,
				Params: [4]float32{0, dashed, float32(op.Paint), opacity(op.Opacity)},
			}, 0)
		case scene.OpShadow:
			if op.Rect.Empty() {
				continue
			}
			sigma := op.Blur / 2
			if sigma < 0.5 {
				sigma = 0
			}
			in := Instance{
				Rect: rect(op.Rect), Radii: scene.Corners(op.Rect, op.Radii, op.Continuous),
				Color: straight(op.Color), Params: [4]float32{1, 0, sigma, opacity(op.Opacity)},
			}
			if !op.Cast.Empty() {
				in.UV, in.Inner = rect(op.Cast), scene.Corners(op.Cast, op.CastRadii, op.Continuous)
			}
			b.add(in, 0)
		case scene.OpGlyphs:
			grad := op.Paint == scene.PaintLinear || op.Paint == scene.PaintOklab
			for _, g := range s.Glyphs[op.Start:op.End] {
				a, kind, contrast := s.MaskAtlas, float32(2), s.Text.Contrast
				switch {
				case g.Colored:
					a, kind = s.ColorAtlas, 3
				case g.Subpixel:
					a, kind, contrast = s.ColorAtlas, 5, s.Text.SubpixelContrast
				}
				if a == nil {
					continue
				}
				boost := float32(0)
				if g.Thin {
					boost = scene.ThinBoost
				}
				aw, ah := float32(a.W), float32(a.H)
				in := Instance{
					Rect:   [4]float32{float32(math.Round(float64(g.X))), float32(math.Round(float64(g.Y))), float32(g.UW), float32(g.VH)},
					Radii:  s.Text.GammaRatios,
					Inner:  [4]float32{contrast, boost},
					UV:     [4]float32{float32(g.U) / aw, float32(g.V) / ah, float32(g.U+g.UW) / aw, float32(g.V+g.VH) / ah},
					Color:  straight(g.Color),
					Params: [4]float32{kind, 0, 0, 1},
				}
				if grad && !g.Colored {
					in.Color, in.Color2, in.Grad = straight(op.Color), straight(op.Color2), op.Gradient
					in.Params[2], in.Params[3] = float32(op.Paint), opacity(op.Opacity)
				}
				b.add(in, 0)
			}
		case scene.OpImage:
			img := op.Image
			if img == nil || op.Rect.Empty() || img.W == 0 || img.H == 0 {
				continue
			}
			tex := image(img)
			if tex == 0 {
				continue
			}
			iw, ih := float32(img.W), float32(img.H)
			gray := float32(0)
			if op.Grayscale {
				gray = 1
			}
			b.add(Instance{
				Rect:   rect(op.Rect),
				Radii:  scene.Corners(op.Rect, op.Radii, op.Continuous),
				UV:     [4]float32{op.Src.X / iw, op.Src.Y / ih, (op.Src.X + op.Src.W) / iw, (op.Src.Y + op.Src.H) / ih},
				Params: [4]float32{4, gray, 0, opacity(op.Opacity)},
			}, tex)
		}
	}
}

// add appends an instance within the current clip, starting a batch when
// the scissor rectangle or the image changes.
func (b *Builder) add(in Instance, image uintptr) {
	cur := &b.stack[len(b.stack)-1]
	if cur.scissor.Empty() {
		return
	}
	in.Clip = rect(cur.rect)
	in.ClipRadii = cur.radii
	n := len(b.Batches)
	if n == 0 || b.Batches[n-1].Scissor != cur.scissor || (image != 0 && b.Batches[n-1].Image != 0 && b.Batches[n-1].Image != image) {
		b.Batches = append(b.Batches, Batch{Start: len(b.Instances), Scissor: cur.scissor, Image: image})
		n++
	} else if image != 0 {
		b.Batches[n-1].Image = image
	}
	b.Instances = append(b.Instances, in)
	b.Batches[n-1].Count++
}

func rect(r scene.Rect) [4]float32 { return [4]float32{r.X, r.Y, r.W, r.H} }

func straight(c scene.Color) [4]float32 {
	return [4]float32{float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255, float32(c.A) / 255}
}

func opacity(o float32) float32 {
	if o == 0 {
		return 1
	}
	return o
}
