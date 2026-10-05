# glass

Liquid Glass for [MyGo](https://github.com/egoist/mygo) apps of native UI,
as macOS 26 and later draw it, on every platform: a material that what is
under an element shows through, frosted and bent along its edges.

```go
import "github.com/egoist/mygo/plugins/glass"

ui.Row(c).Padding(8, 16).Radius(22).Material(glass.Glass{}).Children(func() {
	ui.Text(c, "On glass")
})
```

See [the documentation](../../docs/plugins/glass.md), and the Glass page of
`examples/gallery`.

The glass is an effect of MyGo's renderers (`scene.Effect`): its shader in
Metal Shading Language, HLSL and GLSL (`glass.metal`, `glass.hlsl`,
`glass.glsl`), and its twin for the CPU renderer (`pixels.go`), which must
draw the same pixels; the tests compare each GPU's drawing with the CPU's.
`go generate` compiles the shaders ahead of time where it can: the Metal
library on macOS, with Xcode (`shaders_darwin.go`), and the Direct3D
bytecode on Windows (`shaders_windows.go`). Renderers compile the source
when the code compiled ahead of time is older than it or than their own
part of the shader.
