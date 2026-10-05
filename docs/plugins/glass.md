# Glass

The glass plugin draws Liquid Glass in native UI, the material of macOS 26
and later: what is under an element shows through it, frosted, bent near
its edges as through the rim of a lens, and lit along its rim, over a soft
shadow. MyGo draws it with the plugin's shaders, on the GPU or the CPU, so
it looks the same on macOS, Windows and Linux. It is all Go: no
JavaScript package, and nothing to `mygo.Use`.

```go
import "github.com/egoist/mygo/plugins/glass"

ui.Row(c).Padding(8, 16).Gap(8).Radius(22).Material(glass.Glass{}).Children(func() {
	ui.Text(c, "On glass")
})
```

`glass.Glass` is a material (`ui.Material`): `Material` fills the element
with it in place of a background, and its `Radius` is its shape; its
children draw on it. What shows through is what was painted under it in
the same window: the elements before it, as content that scrolls under a
toolbar placed over it with `Absolute`, and other glass.

```go
ui.Box(c).Fill().Children(func() {
	ui.Scroll(c).Fill().Padding(64, 16, 16).Children(func() { /* ... */ })
	// The toolbar floats over the content, which scrolls under it.
	ui.Row(c).Absolute().Top(12).Left(12).Right(12).Padding(6).Radius(26).Material(glass.Glass{}).Children(func() {
		// ...
	})
})
```

## Styles

- **`glass.Regular`**, the default, frosts what shows through and
  lightens it, or darkens it in dark mode, so that what is on the glass
  reads over anything: bars, buttons and panels. Larger panes are
  frostier, as on macOS.
- **`glass.Clear`** barely blurs or tones what shows through: glass over
  photos and video, where what is on it brings its own contrast.

`Tint` colors the glass toward a color, by its alpha, as a prominent
button: `glass.Glass{Tint: t.Accent}`, with text in `t.AccentText`.
`Interactive` makes the glass grow a little while it is pressed, as
AppKit's interactive glass does on macOS 27: about 1.1 DIPs left and
right and 0.45 above and below, whatever its size, within 150 ms. AppKit
stretches the content too, by a pixel or so, which the children here are
not.

```go
done := ui.Row(c).Padding(8, 16).Radius(20).Material(glass.Glass{Tint: t.Accent, Interactive: true}).Children(func() {
	ui.Text(c, "Done").Bold().TextColor(t.AccentText)
})
if done.Clicked() {
	// ...
}
```

In a drawing, `glass.Paint(p, r, radius, g)` paints a pane of glass over
what the painter painted before it.

## How it looks like macOS

The glass follows macOS 27's, measured from AppKit's `NSGlassEffectView`,
with the optics of the open-source reproductions of Liquid Glass:

- **The bezel.** The surface is flat in the middle and curves down to its
  edges along Apple's squircle profile, over the last 36 DIPs, at most half
  of the pane. Light coming straight down refracts entering it, as through
  glass of refractive index 1.5, so what is near the edge comes from
  further inside: the rim mirrors what is just inside it, as a lens's
  does.
- **The material.** The regular glass blurs what shows through by up to 10
  DIPs, more for larger panes, and maps its lightness, black to 54% and
  white to 100% in light mode, and to 15% and 51% in dark mode, keeping
  its colors. The clear glass adds an eighth to it.
- **The light.** The rim is lit where it faces up or down and shaded where
  it faces the sides, within a DIP of the edge.

## Performance

Each pane reads what is under it, averages and blurs it, then draws with
its own shader: a few small passes on the GPU, and a pane changes when
anything under it does, as content scrolling under a bar. When a window
draws on the CPU, as [Rendering](../ui/rendering.md) describes, a change
under a pane redraws the pane and what its blur reaches around it.

Panes are rounded rectangles. They do not merge into each other when
close, as AppKit's `NSGlassEffectContainerView` does.
