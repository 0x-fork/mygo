package shader

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/egoist/mygo/internal/shaderir"
)

// MaxImages is how many images a shader reads, image0 to image3.
const MaxImages = 4

// Entry is the name of a shader's entry point, which returns the color
// of a pixel, premultiplied:
//
//	func Fragment(dst vec4, pos vec2) vec4
//
// dst is the pixel's center in the target, in its pixels from its top
// left corner, and pos the same point in DIPs from the top left corner of
// the rectangle the shader fills. It may take dst alone, or nothing.
const Entry = "Fragment"

// The uniform variables the bridge declares, which renderers set, before
// the shader's own: the images' sizes in pixels, the size of the
// rectangle the shader fills in DIPs, and the pixels a DIP.
const (
	ImageSizesUniform = "__imageSizes"
	RectSizeUniform   = "__rectSize"
	ScaleUniform      = "__scale"
)

// bridge is the source appended to a shader: the uniform variables that
// only renderers set, the functions that read them and the images, and
// the vertex entry point, which only gives the fragment entry point its
// arguments: renderers draw with vertex stages of their own.
var bridge = func() string {
	var b strings.Builder
	fmt.Fprintf(&b, `
%s

var %s [%d]vec2
var %s vec2
var %s float

// rectSize returns the size of the rectangle the shader fills, in DIPs.
func rectSize() vec2 {
	return %[4]s
}

// pixelScale returns how many pixels of the target make a DIP.
func pixelScale() float {
	return %[5]s
}
`, InternalRegionBegin, ImageSizesUniform, MaxImages, RectSizeUniform, ScaleUniform)
	for i := range MaxImages {
		fmt.Fprintf(&b, `
// image%[1]dSize returns the size of image %[1]d, in its pixels.
func image%[1]dSize() vec2 {
	return %[2]s[%[1]d]
}

// image%[1]dAt returns the pixel of image %[1]d at p, in its pixels from its
// top left corner, premultiplied: transparent outside the image.
func image%[1]dAt(p vec2) vec4 {
	in := step(vec2(0), p) - step(%[2]s[%[1]d], p)
	return __texelAt(__t%[1]d, p) * in.x * in.y
}

// image%[1]dSample returns image %[1]d at p, in its pixels from its top left
// corner, blended between the centers of the four pixels around p, and
// clamped to its edges, as linear filtering samples a texture.
func image%[1]dSample(p vec2) vec4 {
	q := p - 0.5
	f := floor(q)
	w := q - f
	hi := %[2]s[%[1]d] - 1
	a := __texelAt(__t%[1]d, clamp(f, vec2(0), hi))
	b := __texelAt(__t%[1]d, clamp(f+vec2(1, 0), vec2(0), hi))
	c := __texelAt(__t%[1]d, clamp(f+vec2(0, 1), vec2(0), hi))
	d := __texelAt(__t%[1]d, clamp(f+vec2(1), vec2(0), hi))
	return mix(mix(a, b, w.x), mix(c, d, w.x), w.y)
}
`, i, ImageSizesUniform)
	}
	fmt.Fprintf(&b, `
func %s(dst vec2, pos vec2) (vec4, vec2) {
	return vec4(dst, 0, 1), pos
}

%s
`, vertexEntry, InternalRegionEnd)
	return b.String()
}()

const vertexEntry = "__vertex"

// CompileFragment compiles a MyGo shader: Go's syntax in package main,
// with uniform variables (exported, package level) that the app sets and
// the entry point Fragment, which may call functions of its own and read
// images through the functions image0At to image3At, image0Sample to
// image3Sample and image0Size to image3Size.
func CompileFragment(src []byte) (*shaderir.Program, error) {
	var b bytes.Buffer
	b.Write(src)
	b.WriteString(bridge)
	p, err := Compile(b.Bytes(), vertexEntry, Entry, MaxImages)
	if err != nil {
		return nil, err
	}
	if p.FragmentFunc.Block == nil {
		return nil, fmt.Errorf("shader: no entry point %s", Entry)
	}
	p.FragmentSource = bytes.Clone(src)
	return p, nil
}
