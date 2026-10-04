# Overlays

Overlays show above the window's content: [dialogs](dialog.md) over the
dimmed window, [alert dialogs](alert-dialog.md), [popovers](popover.md)
below an element, the popups of [selects](select.md) and
[comboboxes](combobox.md), [tooltips](tooltip.md) and [toasts](toast.md).
They share how they treat the keyboard and what is behind them.

```go
more := ui.Button(c, "More ▾")
if more.Clicked() {
	app.menu = !app.menu
}
ui.Popover(c, more, &app.menu, func() {
	if ui.Button(c, "Rename").Clicked() {
		app.menu, app.renaming = false, true
	}
})
ui.Modal(c, &app.renaming, func() {
	ui.Text(c, "Rename").Bold()
	if ui.TextInput(c, &app.name).AutoFocus().Submitted() {
		app.renaming = false
	}
})
```

## The keyboard

A dialog keeps the keyboard: it takes the focus as it opens, on its first
element that takes it unless one in it asked for it (`AutoFocus`), Tab goes
round its elements, and the window's shortcuts built outside it wait, as
what is behind it is inert, which screen readers do not see either. A
popover's elements follow its anchor as Tab moves.

Escape closes the overlay on top, a select's popup before the dialog it is
in, unless the focused element takes it, as a terminal does; and an
overlay that closes with the focus in it gives the focus back to the
element that had it as it opened, its button say.

## Your own overlays

`ui.Overlay` builds elements above everything else, placed with `Absolute`
in DIPs of the window:

```go
ui.Overlay(c, func() {
	ui.Text(c, "Offline").Absolute().Top(12).Right(12).
		Padding(4, 10).Radius(999).Background(t.Danger).TextColor(t.AccentText)
})
```

`PopoverBase` and `DialogBase` are a popover and a dialog without a look,
for overlays of your own design: see [custom widgets](custom-widgets.md).

Native [dialogs](../native.md#dialogs) work too: call them from a goroutine,
so that the view does not wait for them.
