# Popover

`ui.Popover` shows a panel its function builds below an element, its
anchor, while a `*bool` is true; clicking outside it or pressing Escape
sets it to false. Where there is no room below the anchor, the panel shows
above it.

```go
more := ui.Button(c, "More ▾")
if more.Clicked() {
	app.menu = !app.menu
}
ui.Popover(c, more, &app.menu, func() {
	if ui.Button(c, "Rename").Clicked() {
		app.menu, app.renaming = false, true
	}
	if ui.Button(c, "Duplicate").Clicked() {
		app.menu = false
		app.duplicate()
	}
})
```

A popover's elements follow its anchor as Tab moves, and as it closes with
the focus in it, it gives the focus back to the element that had it. See
[overlays](overlays.md).

For a menu of the system's, use a [menu button](menu-button.md); for a tip,
a [tooltip](tooltip.md).

## Without a look

`ui.PopoverBase` is a popover without a look: its function styles the panel
and builds its content. A top margin keeps the panel apart from the anchor,
on either side:

```go
ui.PopoverBase(c, anchor, &app.open, func(panel *ui.Element) {
	panel.Margin(6, 0, 0, 0).Padding(8).Radius(12).Background(t.Background).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.2))
	app.filters(c)
})
```

See [custom widgets](custom-widgets.md).

## Accessibility

Assistive technology sees the panel as a popup after its anchor.
