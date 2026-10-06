# Toast

`c.Toast` shows a message near the bottom of the window for a few seconds,
as the outcome of what the user just did:

```go
if ui.Button(c, "Save").Clicked() {
	app.save()
	c.Toast("Saved")
}
```

`c.ToastAction` shows one with a button, as Undo after deleting, for longer:
the button runs the action, on the main thread as the view does, and closes
the toast; Tab reaches it.

```go
app.trash(note)
c.ToastAction("Note deleted", "Undo", func() { app.restore(note) })
```

A message already showing shows anew; others stack above it. A toast stays
while the pointer rests on it, and fades in and out.

A toast takes the theme's `Inverse` and `InverseText` colors, its text and
background turned over unless the theme sets them (see
[themes](styling.md#themes)).

## Accessibility

Screen readers read a toast's text as it shows (see
[announcements](accessibility.md#announcements)); assistive technology sees
it as a status, which holds its button.
