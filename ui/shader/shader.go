// Package shader draws with shaders an app writes: fragment shaders in
// Go's syntax, which run on the GPU, whichever renderer draws the window
// (Metal, Direct3D 11, OpenGL or WebGL 2), and on the CPU where none can.
//
// A shader is a Go file of package main whose function Fragment returns
// the color of a pixel, premultiplied, from its position:
//
//	package main
//
//	var Time float
//
//	func Fragment(dst vec4, pos vec2) vec4 {
//		v := 0.5 + 0.5*sin(pos.x/20+Time)
//		return vec4(v, v, 1, 1)
//	}
//
// dst is the pixel's center in pixels of the window (or the canvas drawn
// into), and pos the same point in DIPs from the top left corner of the
// rectangle the shader fills. The language is Ebitengine's Kage, whose
// compiler MyGo's is forked from: Go's syntax with the types bool, int,
// uint, float, vec2 to vec4, ivec2 to ivec4, uvec2 to uvec4, mat2 to mat4
// and arrays of them, GLSL's built-in functions (sin, mix, smoothstep,
// dot, ...), functions of its own, if, and for loops. Exported variables
// at the package level are uniforms, which Options.Uniforms sets.
//
// A shader reads up to four images, bitmaps or canvases (ui.Canvas),
// through these functions, in the images' pixels from their top left
// corners:
//
//	image0At(p vec2) vec4      // the pixel at p, transparent outside
//	image0Sample(p vec2) vec4  // blended between the pixels around p
//	image0Size() vec2          // the size in pixels
//
// and image1At to image3At and so on. rectSize returns the size of the
// rectangle the shader fills, in DIPs, and pixelScale the pixels a DIP.
//
// A shader that draws into a canvas makes the input of another, as the
// passes of an effect: one computes a field into a small canvas, the next
// reads it to draw the window.
package shader

import (
	"fmt"
	"math"
	"reflect"

	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/shader"
	"github.com/egoist/mygo/internal/shaderir"
	"github.com/egoist/mygo/internal/shaderir/interp"
	"github.com/egoist/mygo/ui"
)

// Shader is a compiled shader, which draws in any window, from any
// goroutine's views.
type Shader struct {
	prog     *interp.Program
	fx       *scene.Effect
	uniforms map[string]uniform
	words    int
}

// uniform is a uniform variable of a shader: its type, and its lanes in
// the words of the uniforms.
type uniform struct {
	t       shaderir.Type
	word, n int
}

// The bridge's uniforms, which Draw sets: the images' sizes, the
// rectangle's size and the pixels a DIP, the first words.
var bridge = []string{shader.ImageSizesUniform, shader.RectSizeUniform, shader.ScaleUniform}

const scaleWord = 2*shader.MaxImages + 2

// Compile compiles a shader, src the source of a Go file as the package's
// documentation describes. It reports the errors of the source with their
// lines.
func Compile(src string) (*Shader, error) {
	p, err := shader.CompileFragment([]byte(src))
	if err != nil {
		return nil, err
	}
	for i, name := range bridge {
		if i >= len(p.UniformNames) || p.UniformNames[i] != name {
			return nil, fmt.Errorf("shader: the uniforms start with %v, not %v", p.UniformNames, bridge)
		}
	}
	prog, err := interp.New(p)
	if err != nil {
		return nil, err
	}
	s := &Shader{prog: prog, uniforms: map[string]uniform{}}
	for i, n := range interp.UniformWords(p) {
		s.uniforms[p.UniformNames[i]] = uniform{t: p.Uniforms[i], word: s.words, n: n}
		s.words += n
	}
	s.fx = &scene.Effect{Name: "shader", Pixels: func() scene.EffectPixels { return &pixels{prog: prog} }}
	lanes, size := code(p, s.fx)
	l := &scene.ShaderLayout{Size: size}
	for _, at := range lanes {
		l.Lanes = append(l.Lanes, int32(at))
	}
	s.fx.Shader = l
	return s, nil
}

// MustCompile is Compile for a shader known to compile, as a package's
// variable: it panics when it does not.
func MustCompile(src string) *Shader {
	s, err := Compile(src)
	if err != nil {
		panic(err)
	}
	return s
}

// Options are what a shader draws with.
type Options struct {
	// Uniforms are the values of the shader's uniform variables, by name:
	// a number of any of Go's kinds (bool, an integer, a float) for one of
	// a scalar type, or a slice or an array of numbers, or a ui.Color (its
	// components from 0 to 1, not premultiplied), for a vector, a matrix
	// (by columns) or an array (elements in order); a variable missing is
	// zero, and a name the shader has not is left out. Integers set ints
	// and uints by their low 32 bits, as Go converts them.
	Uniforms map[string]any
	// Images are the images image0 to image3 read: bitmaps, or canvases'
	// (ui.Canvas.Bitmap); nil for none, which reads as transparent.
	Images [4]*ui.Bitmap
	// Radius rounds the corners of the rectangle, in DIPs.
	Radius float32
}

// Draw fills r with the shader, in DIPs of p, as p's other drawings are:
// clipped, transformed and faded with the painter.
func (s *Shader) Draw(p *ui.Painter, r ui.Rect, o Options) {
	p.ShaderEffect(s.fx, r, o.Radius, s.lanes(p, r, o), o.Images)
}

// Fill paints the text, paths and icons that fn paints with p with the
// shader in place of their colors, as a gradient fills them: each pixel
// the shader's color times the shape's coverage, the shader filling box,
// in DIPs, whose corner its positions start from and whose size rectSize
// returns. Emoji keep their colors, and the rest fn paints, as rectangles
// and images, paints as it would. Options.Radius does not apply.
func (s *Shader) Fill(p *ui.Painter, box ui.Rect, o Options, fn func()) {
	p.ShaderFill(s.fx, box, s.lanes(p, box, o), o.Images, fn)
}

// FillPath fills path with the shader (non-zero winding), the shader
// filling the path's box (Path.Bounds).
func (s *Shader) FillPath(p *ui.Painter, path *ui.Path, o Options) {
	s.Fill(p, path.Bounds(), o, func() { p.FillPath(path, white) })
}

// StrokePath strokes the outline of path with the shader, width DIPs
// wide, with round joins and caps, the shader filling the path's box
// (Path.Bounds) grown by half the width.
func (s *Shader) StrokePath(p *ui.Painter, path *ui.Path, width float32, o Options) {
	b := path.Bounds()
	h := width / 2
	s.Fill(p, ui.Rect{X: b.X - h, Y: b.Y - h, W: b.W + width, H: b.H + width}, o, func() { p.StrokePath(path, width, white) })
}

// Text draws a line of text with its top-left corner at (x, y), size DIPs
// tall, filled with the shader, which fills the text's box.
func (s *Shader) Text(p *ui.Painter, x, y float32, text string, size float32, o Options) {
	w, h := p.MeasureText(0, ui.Span{Text: text, Size: size})
	s.Fill(p, ui.Rect{X: x, Y: y, W: w, H: h}, o, func() { p.Text(x, y, text, size, white) })
}

// lanes returns the words of the uniforms' lanes of the shader drawn with o
// in r, the box it fills, with the bridge's first.
func (s *Shader) lanes(p *ui.Painter, r ui.Rect, o Options) []uint32 {
	words := make([]uint32, s.words)
	for i, b := range o.Images {
		if b != nil {
			w, h := b.Size()
			words[2*i], words[2*i+1] = math.Float32bits(float32(w)), math.Float32bits(float32(h))
		}
	}
	words[scaleWord-2], words[scaleWord-1] = math.Float32bits(r.W), math.Float32bits(r.H)
	words[scaleWord] = math.Float32bits(p.Scale())
	for name, v := range o.Uniforms {
		if u, ok := s.uniforms[name]; ok && name[0] != '_' {
			kind := u.t.Scalar()
			if u.t.Main == shaderir.Array {
				kind = u.t.Sub[0].Scalar()
			}
			set(words[u.word:u.word+u.n], kind, name, v)
		}
	}
	return words
}

// set writes value v of uniform name into its lanes, as numbers of kind:
// a number, a slice or an array of numbers of any of Go's kinds, or a
// ui.Color. Integers convert to int and uint keeping their low 32 bits, as
// Go converts them; floats drop their fraction.
func set(lanes []uint32, kind shaderir.BasicType, name string, v any) {
	if c, ok := v.(ui.Color); ok {
		for i, x := range [4]uint8{c.R, c.G, c.B, c.A} {
			if i < len(lanes) {
				lanes[i] = lane(kind, reflect.ValueOf(float64(x)/255))
			}
		}
		return
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len() && i < len(lanes); i++ {
			if !number(rv.Index(i)) {
				panic(fmt.Sprintf("shader: the uniform %s is set to a %T, not numbers", name, v))
			}
			lanes[i] = lane(kind, rv.Index(i))
		}
	default:
		if !rv.IsValid() || !number(rv) {
			panic(fmt.Sprintf("shader: the uniform %s is set to a %T, not a number, numbers or a ui.Color", name, v))
		}
		if len(lanes) > 0 {
			lanes[0] = lane(kind, rv)
		}
	}
}

// number reports whether x is a bool, an integer or a float.
func number(x reflect.Value) bool {
	switch x.Kind() {
	case reflect.Bool, reflect.Float32, reflect.Float64,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	}
	return false
}

// lane returns the number x as a lane of kind.
func lane(kind shaderir.BasicType, x reflect.Value) uint32 {
	var i int64
	var f float64
	switch x.Kind() {
	case reflect.Bool:
		if x.Bool() {
			i, f = 1, 1
		}
	case reflect.Float32, reflect.Float64:
		f = x.Float()
		i = int64(f)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		i, f = int64(x.Uint()), float64(x.Uint())
	default:
		i, f = x.Int(), float64(x.Int())
	}
	switch kind {
	case shaderir.Float:
		return math.Float32bits(float32(f))
	case shaderir.Bool:
		if f != 0 {
			return 1
		}
		return 0
	}
	return uint32(i)
}

// pixels draws a shader on the CPU, with the interpreter.
type pixels struct {
	prog  *interp.Program
	in    interp.Inputs
	rect  scene.Rect
	scale float32
}

func (x *pixels) Begin(op *scene.EffectOp, rect scene.Rect, radii [4]float32) {
	x.rect = rect
	x.in.Uniforms = op.Uniforms
	x.in.Images = x.in.Images[:0]
	for _, img := range op.Images {
		if img == nil || len(img.Pix) < 4*img.W*img.H {
			x.in.Images = append(x.in.Images, nil)
		} else {
			x.in.Images = append(x.in.Images, sceneImage{img})
		}
	}
	x.scale = 1
	if len(op.Uniforms) > scaleWord {
		x.scale = math.Float32frombits(op.Uniforms[scaleWord])
	}
}

func (x *pixels) Color(px, py float32, _ *scene.BackdropImage) [4]float32 {
	c, ok := x.prog.Color(&x.in, [4]float32{px, py, 0, 1}, (px-x.rect.X)/x.scale, (py-x.rect.Y)/x.scale)
	if !ok {
		return [4]float32{}
	}
	return c
}

// sceneImage is an image of a scene as the interpreter reads it.
type sceneImage struct{ img *scene.Image }

func (s sceneImage) Size() (int, int) { return s.img.W, s.img.H }

func (s sceneImage) At(x, y int) [4]float32 {
	p := s.img.Pix[4*(y*s.img.W+x):][:4]
	return [4]float32{float32(p[0]) / 255, float32(p[1]) / 255, float32(p[2]) / 255, float32(p[3]) / 255}
}

// white is the color the shapes a shader fills are painted with, whose
// coverage it multiplies.
var white = ui.Color{R: 255, G: 255, B: 255, A: 255}
