# Spinner

`ui.Spinner` creates an indicator of work of unknown length, as AppKit's
spinning progress indicator: spokes turning, in the theme's muted text
color, a little higher than the theme's font size.

```go
ui.Row(c).Gap(8).Children(func() {
	ui.Spinner(c).Label("Loading")
	ui.Text(c, "Loading messages…")
})
```

`Size` makes it larger, as `Size(32, 32)` in an empty view. It goes on
turning when the desktop asks for less motion, as the system's spinners do:
it shows that something is going on.

## Accessibility

Assistive technology sees a progress indicator of unknown length, named by
its `Label`.
