# Tooltip

`Tooltip` shows a tip once the pointer rests on an element, and describes
the element to assistive technology:

```go
ui.Button(c, "").Label("Share").Tooltip("Share with others").Children(func() {
	ui.Icon(c, share)
})
```

The tip shows once the pointer has rested on the element for 0.6 seconds,
below and to the right of it, kept in the window, and goes as the pointer
leaves or presses. Over elements inside one another, the innermost one's
tip shows. Give one to buttons showing only an icon, and to what a label
alone does not explain.

## Accessibility

Assistive technology reads the tip as the element's description, after its
name, as help text, unless `Description` gives another.
