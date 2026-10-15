# Shaders

A shader computes the color of every pixel of a rectangle, on the GPU,
from its position, values the app passes it each frame and images it
reads: noise and gradients that move, a texture lit pixel by pixel, a
ripple across the window. Package `ui/shader` compiles shaders written in
Go's syntax, and draws them on whichever GPU renderer draws the window
(Metal, Direct3D 11, OpenGL or WebGL 2), and on the CPU where none can,
the same pixels within rounding.

```go
import "github.com/egoist/mygo/ui/shader"

var waves = shader.MustCompile(`package main

var Time float

func Fragment(dst vec4, pos vec2) vec4 {
	v := 0.5 + 0.5*sin(pos.x/20+Time)
	return vec4(v*0.2, v*0.4, v, 1)
}
`)

ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
	waves.Draw(p, r, shader.Options{
		Uniforms: map[string]any{"Time": p.Now().Sub(start).Seconds()},
	})
	p.AnimationFrame()
})
```

`Draw` fills a rectangle of the painter, clipped, transformed and faded
with it as anything it paints; `Options.Radius` rounds its corners.

## Text, paths and icons

A shader also fills shapes in place of a color, as a gradient does, and
as SwiftUI fills a shape or text with a shader: `FillPath` and
`StrokePath` paint a `ui.Path`, `Text` a line of text, and `Fill`
whatever text, paths and icons a function paints, in a box of your own:

```go
var circle ui.Path
circle.Circle(60, 60, 50)

ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
	opts := shader.Options{Uniforms: map[string]any{"Time": t}}
	waves.FillPath(p, &circle, opts)
	waves.Text(p, 140, 40, "Hello", 48, opts)
	waves.Fill(p, ui.Rect{X: 140, Y: 100, W: 200, H: 40}, opts, func() {
		p.RichText(140, 100, 200, ui.Span{Text: "and more", Size: 24, Weight: 700})
		p.Icon(star, ui.Rect{X: 300, Y: 100, W: 24, H: 24}, ui.RGB(0, 0, 0))
	})
})
```

Each pixel shows the shader's color times how much of it the shape
covers, the edges of text corrected as the platform's text is for that
color. `pos` runs across the shape's box (the path's, grown by half the
stroke's width, the text's, or the one `Fill` takes), which `rectSize()`
returns; emoji keep their own colors, and rectangles and images `Fill`'s
function paints paint as they would.

## The language

A shader is a Go file of package main. Its function `Fragment` returns the
pixel's color, premultiplied (each color component multiplied by the
alpha):

```go
func Fragment(dst vec4, pos vec2) vec4
```

`dst` is the pixel's center in pixels of the window, or of the canvas
drawn into, and `pos` the same point in DIPs from the rectangle's top
left corner. `Fragment` may take `dst` alone, or nothing.

The language is [Ebitengine's Kage](https://ebitengine.org/en/documents/shader.html),
whose compiler MyGo's is forked from: Go's syntax, with these types:

| Types | |
| --- | --- |
| `bool`, `int`, `uint`, `float` | scalars of 32 bits |
| `vec2`, `vec3`, `vec4` | vectors of floats, with swizzles (`v.xy`, `c.rgb`) |
| `ivec2` to `ivec4`, `uvec2` to `uvec4` | vectors of ints and uints |
| `mat2`, `mat3`, `mat4` | matrices of floats, by columns |
| `[N]T` | arrays of any of them |

Functions of your own, `if`, `for` loops (to bounds that need not be
constants, and `range` over an int), constants, and GLSL's built-in
functions: `sin`, `cos`, `atan2`, `pow`, `exp`, `sqrt`, `abs`, `floor`,
`fract`, `mod`, `min`, `max`, `clamp`, `mix`, `step`, `smoothstep`,
`length`, `distance`, `dot`, `cross`, `normalize`, and the others. ints
and uints are two kinds of integers that mix only in shifts, as in Go;
uints wrap around, which hashes need:

```go
func pcg(v uint) uint {
	s := v*747796405 + 2891336453
	w := ((s >> ((s >> 28) + 4)) ^ s) * 277803737
	return (w >> 22) ^ w
}
```

`discard()` leaves the pixel as it was. `dfdx`, `dfdy` and `fwidth` give
how a value changes to the next pixel on the GPU, and zero on the CPU.

## Uniforms

Exported variables at the package level are uniforms: values the app sets
for each draw in `Options.Uniforms`, by name. A number sets a scalar; a
slice or an array of numbers sets a vector, a matrix (by columns) or an
array (elements in order); a `ui.Color` sets a `vec4`, its components from
0 to 1. A uniform the options leave out is zero.

```go
var Taps [6]vec4 // x, y, when, how hard
var Count int
```

```go
taps := make([]float32, 0, 24)
for _, t := range ripples {
	taps = append(taps, t.x, t.y, t.at, t.force)
}
fx.Draw(p, r, shader.Options{Uniforms: map[string]any{"Taps": taps, "Count": len(ripples)}})
```

## Images

A shader reads up to four images, `Options.Images`: bitmaps, or canvases'
(`Canvas.Bitmap`), in their pixels from their top left corners:

| Function | |
| --- | --- |
| `image0At(p vec2) vec4` | the pixel at `p`, transparent outside the image |
| `image0Sample(p vec2) vec4` | blended between the four pixels around `p`, clamped to the edges |
| `image0Size() vec2` | the image's size in pixels |

and `image1At` to `image3At`, and so on. `rectSize()` returns the size of
the rectangle the shader fills, in DIPs, and `pixelScale()` the pixels a
DIP.

A shader drawing into a [canvas](drawing.md#canvases) makes the input of
another, as the passes of an effect do: one computes a field at a few
samples into a small canvas, the next reads it, sampled, at every pixel of
the window.

```go
field := ui.NewCanvas(cols, rows)

box.Draw(func(p *ui.Painter, r ui.Rect) {
	field.Draw(p, func(cp *ui.Painter) {
		windShader.Draw(cp, ui.Rect{W: float32(cols), H: float32(rows)}, shader.Options{Uniforms: wind})
	})
	glyphShader.Draw(p, r, shader.Options{Images: [4]*ui.Bitmap{field.Bitmap(), atlas}})
})
```

## Errors and cost

`Compile` reports what is wrong with a shader with its lines; compile
shaders once, as package variables with `MustCompile`. `Compile` takes
about a millisecond: it checks the shader and translates it into the
GPU's language (Metal, HLSL or GLSL), and the GPU's driver compiles that
when a window first draws the shader, which delays that frame. On macOS,
that is about 100 ms, the first time the app runs on the Mac; Metal keeps
what it compiled on disk, so later launches draw at once. Linux's Mesa
drivers keep theirs on disk too (in `~/.cache/mesa_shader_cache`): from
a tenth to a third of a second on the CPU's llvmpipe, the first time,
and a few milliseconds after. On Windows, Direct3D's compiler takes from
a tenth of a second for a short shader to a second for one of a few
hundred lines, so `Compile` starts it on a goroutine of its own: the
renderers of every window share the code, waiting on their first draw
only for what is left, and it stays in the user's cache directory
(`MyGo\ShaderCache`) for later launches. A shader costs
little to draw on a GPU, whatever it computes per pixel; on the CPU (the
renderer of last resort, and `ui.Tester`) it runs interpreted, a few
microseconds a pixel: seconds for a window. A shader covering much of the
window can ask `p.Accelerated()`, which is false there and where the GPU's
renderer draws on the CPU (WARP, llvmpipe, SwiftShader), and draw less,
or nothing:

```go
ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
	if !p.Accelerated() {
		return // a backdrop, which the window does without
	}
	backdrop.Draw(p, r, shader.Options{})
})
```

`Draw` panics on a uniform set to a value that is
not a number.
