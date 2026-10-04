# Native UI

A window can show a user interface that MyGo draws itself instead of a web
page. You write it in Go with package `ui`: there is no HTML, no JavaScript
and no frontend build, and the window starts no webview, so it opens at
once and uses little memory. MyGo draws it on the GPU, with Metal on macOS,
Direct3D 11 on Windows and OpenGL on Linux.

```go
package main

import (
	"fmt"
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type counter struct{ n int }

func (s *counter) view(c *ui.Context) {
	ui.Column(c).Fill().Center().Gap(12).Children(func() {
		ui.Text(c, fmt.Sprint(s.n)).FontSize(40).Bold()
		if ui.PrimaryButton(c, "Increment").Clicked() {
			s.n++
		}
	})
}

func main() {
	s := &counter{}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "Counter",
			Width:   320,
			Height:  240,
			Content: ui.View(s.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
```

`mygo init -template native my-app` starts a project of native UI (see
[the CLI](cli.md#mygo-init)). In a clone of the repository,
`examples/counter-native` is a counter with a test of its view
(`go run ./examples/counter-native`), and `go run ./examples/gallery`
tours what the toolkit does.

One app can have windows of both kinds. Native UI suits tools, settings,
inspectors and utilities, and apps that must start instantly; a web page
suits rich documents, existing web code and anything that needs what only
a browser has. Screen readers and other assistive technology read native
UI as they read other apps (see [Accessibility](#accessibility)).

## Views

The view is a function from your app's state to its interface. MyGo calls
it on the main thread to build every frame: after input, after you change
the state (see [below](#change-the-state-from-other-goroutines)), and while
something animates. Elements live for one frame: the state that lasts is
yours, in your own types, plus what MyGo keeps for each element from frame
to frame (focus, hover, scrolling, the text being edited, animations).

Events are questions you ask while building: `Clicked` reports whether the
element was clicked since the last frame, so the code that handles a click
sits where the button is built:

```go
if ui.Button(c, "Delete").Clicked() {
	app.items = slices.Delete(app.items, i, i+1)
}
```

When a handler changes the state while the view builds, MyGo builds the
frame again, so it always shows the outcome.

MyGo tells elements apart by their position among their siblings. When the
siblings before an element can change, as in a list whose items you
insert, delete or reorder, give each item a `Key` so its state follows it:

```go
for i := range app.todos {
	todo := &app.todos[i]
	ui.Row(c).Key(todo.ID).Children(func() {
		ui.Checkbox(c, &todo.Done, todo.Title)
	})
}
```

Widgets that handle their input as they are created, such as `Checkbox`
here, take the key from an element around them: `Key` panics on them, as
their state would be lost.

### Change the state from other goroutines

The view reads your state on the main thread. Change it from other
goroutines with `Window.Update`, which runs a function on the main thread
and then draws a new frame:

```go
go func() {
	items, err := fetchItems()
	win.Update(func() { app.items, app.err = items, err })
}()
```

`Window.Invalidate` only draws a new frame, for state you guard yourself.
Within the view, `c.Invalidate()` asks for another frame and `c.After(d)` for
one after a delay, such as a clock's next second.

## Layout

Elements lay out their children with flexbox, as in CSS, or in a
[grid](#grids), in device-independent pixels (DIPs):

- `ui.Column` stacks its children from top to bottom and stretches them to
  its width; `ui.Box` is a column too. `ui.Row` places them from left to
  right and centers them vertically. `Reverse` lays them out the other
  way, from the right or the bottom, as CSS's `row-reverse` and
  `column-reverse`.
- `Width`, `Height` and `Size` set sizes; `WidthPercent` and `HeightPercent`
  take a share of the parent; `MinWidth`, `MaxWidth`, `MinHeight` and
  `MaxHeight` bound them, and their `Percent` forms by a share of the
  parent; `Fill`, `FillWidth` and `FillHeight` take the parent's whole
  content box.
- `Grow(1)` gives an element the free space along its parent's direction,
  like `flex: 1`: a list that fills the rest of a window, or a `Spacer` that
  pushes the elements after it to the end. `Shrink`, `Basis` and
  `BasisPercent` work as in CSS.
- `Gap` spaces the children, `GapX` and `GapY` horizontally and vertically
  apart; `Padding` and `Margin` take one, two or four values, as in CSS, and
  `PaddingX`, `PaddingY`, `MarginX` and `MarginY` two sides. A margin of
  `ui.Auto` takes the free space on its side, as in CSS: `Margin(0, ui.Auto)`
  centers an element in a column, and `Margin(0, 0, 0, ui.Auto)` sends a
  row's child, and those after it, to the end.
- `Justify` places the children along the direction (`Start`, `Center`,
  `End`, `SpaceBetween`, `SpaceAround`, `SpaceEvenly`), `AlignItems` across
  it (`Start`, `Center`, `End`, `Stretch`), `AlignSelf` one child; `Center`
  centers them both ways. `Wrap` wraps a row onto more lines, which
  `WrapReverse` stacks upward, and `AlignContent` places those lines (as
  `Justify` places children, or `Stretch`).
- `Absolute` takes an element out of the flow, placed with `Top`, `Right`,
  `Bottom` and `Left` in its parent, in DIPs or with `TopPercent` and the
  like; automatic margins center it between the sides it is placed from.
  On an element in the flow, `Top` and the others move it from where the
  layout put it, without moving its siblings, as CSS's relative
  positioning does: a badge raised a little above its text.
- `AspectRatio` keeps an element's proportions; `Clip` cuts its children
  to its rounded box, inside its border, which it draws over them;
  `ClipX` and `ClipY` clip only on the sides or above and below.
- `Invisible` hides an element and its children, which keep their room
  but draw nothing and take neither the pointer nor the focus.
- `Debug` outlines an element and everything inside it, with their padding
  and margins, to see why the layout is what it is.

`ui.Scroll` and `ui.ScrollHorizontal` scroll their children with the wheel,
the touchpad, their scroll bar and the keyboard: the arrow keys, Page Up
and Page Down, Space, Home and End scroll the container around the focus,
or under the pointer, unless the focused element takes those keys, as a
text input does. `ui.ScrollBoth` scrolls both ways, as a canvas or a wide
table does. Give them a size, or grow them in their parent. For rows of a
collection, see [lists](#lists).

A scroll container keeps its offset as it keeps its other state, by its
place in the view or its `Key`. To read the offset, set it or keep it in
the app's state, give the container a `ui.ScrollState` with `TrackScroll`:
the container shows its content from the state's `X` and `Y`, writes them
as the user scrolls, and sets `MaxX` and `MaxY` to how far they go. `Y = 0`
scrolls to the top and `math.MaxFloat32` to the end, and a `ScrollState`
for each page a container shows keeps each page's place. A log that follows
its end, unless the user scrolled up from it:

```go
if app.log.Y >= app.log.MaxY {
	app.log.Y = math.MaxFloat32
}
ui.Scroll(c).TrackScroll(&app.log).Grow(1).Children(app.lines)
```

`ScrollIntoView` scrolls the containers around an element as little as
shows it, once the frame is laid out, so that it works in the frame that
adds the element: a new message, or the item the keys chose. Call it in
that frame only, or the user could not scroll the element away. Rows a
`List` has not built scroll into view with its `ListState`.

### Lists

`ui.List` shows rows of a collection of any length: it builds only the
rows in view, and a few beyond, so a list of millions of rows is as fast
as one of ten. Rows take the height of their content, which may differ
from row to row: the list measures rows as they show and estimates the
others from them.

```go
ui.List(c, nil, len(app.files), func(i int) {
	ui.Text(c, app.files[i].Name).Padding(6, 12)
}).Grow(1)
```

`Gap` spaces the rows, `Padding` pads the content and scrolls with it, and
`Justify(ui.End)` puts rows that do not fill the list at its bottom, as a
chat's first messages. What else you build in a list shows while it has no
rows, as a message that it is empty:

```go
ui.List(c, nil, len(results), func(i int) {
	ui.Text(c, results[i].Title).Padding(6, 12)
}).Grow(1).Children(func() {
	if len(results) == 0 {
		ui.Text(c, "No results").TextColor(c.Theme().TextMuted).Padding(12)
	}
})
```

A list without a size, or with only a `MaxHeight`, is as high as its rows.

A list keeps its place by a row rather than by an offset, so the rows in
view stay where they are while the rows around them are measured, grow,
come and go, and its offsets count millions of rows to a fraction of a
DIP. Give it a `ui.ListState` in your state, in place of `nil`, to say how
it behaves and to move it:

- `Key` returns an identity for the item of a row, such as its ID: the
  state of a row (focus, text being edited, …) follows its item, and the
  list keeps its place, and its choice, when rows are added or removed
  above them, as when older messages load.
- `FollowEnd` starts the list at its end, and keeps the end in view as rows
  come or grow, until the user scrolls away from it, as a chat or a log
  does; scrolling back to the end follows it again.
- `Selected` lets a click, or Up, Down, Home and End while the list has the
  keyboard focus (and Page Up and Page Down on Linux and Windows), choose a
  row, which shows in the accent color; the list's `Changed` reports a new
  choice, and `Submitted` a double click or Enter.
- `Selection` lets the user choose several rows, held in a
  `ui.Selection[K]` by the keys of their items (`Key`), or by their indices
  without one: a click chooses one, Cmd-click (Ctrl-click on Linux and
  Windows) adds a row or takes it out, and Shift-click or Shift with the
  keys chooses the rows from the one last chosen; Cmd+A chooses all. On
  Linux and Windows, Ctrl with the keys moves without choosing, and
  Ctrl+Space adds the row there or takes it out. `Selected`, set as well,
  is the row last chosen.
- `Label` returns the text of a row: typing its first letters while the
  list has the focus chooses it, as in Finder or Explorer, and assistive
  technology reads it as the row's name.
- `Header` names the rows heading sections: the header of the section at
  the top stays pinned there while its rows scroll under it, until the next
  header pushes it away. Give headers a background.
- `ScrollTo(row, align)` shows a row at the `ui.Start`, `ui.Center` or
  `ui.End` of the list, `ScrollIntoView(row)` scrolls as little as shows it,
  and `ScrollToEnd` scrolls to the end, rows not built yet included: the list
  scrolls as the frame is laid out, to where the row is once measured.
  `Visible` returns the first and last rows in view and `AtEnd` whether the
  end shows, to load more as the list nears the end of what was loaded.

Files to choose several of, by their paths:

```go
type app struct {
	files  []File
	chosen ui.Selection[string]
	list   ui.ListState
}

app.list.Key = func(i int) any { return app.files[i].Path }
app.list.Label = func(i int) string { return app.files[i].Name }
app.list.Selection = &app.chosen
ui.List(c, &app.list, len(app.files), func(i int) {
	ui.Text(c, app.files[i].Name).Padding(6, 12)
}).Grow(1)
if ui.Button(c, fmt.Sprintf("Delete %d", app.chosen.Len())).Clicked() {
	for path := range app.chosen.All() {
		app.delete(path)
	}
	app.chosen.Clear()
}
```

The keys of items gone from the list stay in the selection until you take
them out. `Element.ClickModifiers` gives the modifier keys of any click, for
widgets of your own that do the same.

A chat:

```go
app.chat.Key = func(i int) any { return app.messages[i].ID }
app.chat.FollowEnd = true
if first, _ := app.chat.Visible(); first < 5 && app.more {
	app.loadOlder() // above the messages in view, which stay put
}
if !app.chat.AtEnd() && ui.Button(c, "Jump to latest").Clicked() {
	app.chat.ScrollToEnd()
}
ui.List(c, &app.chat, len(app.messages), func(i int) {
	message(c, app.messages[i])
}).Grow(1).Justify(ui.End)
```

The row holding the keyboard focus stays built when it scrolls out of
view, so that what is being edited in it stays, and Tab moves the focus
from row to row, scrolling them into view.

### Tables

`ui.Table` puts a list's rows in columns, under a header of
`ui.TableColumn`s, each with a title, a width (or 0 to share the room the
others leave) and an alignment; `cell` builds the content of a row's
column. Rows are as high as their tallest cell, so text that wraps makes
its row taller. The table takes the same `ListState` as a list, or `nil`,
and takes the keyboard focus for it: with `Selected`, a click or the keys
choose a row, with `Selection` several, and `Submitted` reports a double
click or Enter. A row
`Header` names spans every column, which `cell` builds as column 0, and
stays at the top while its section's rows scroll under it.

```go
cols := []ui.TableColumn{{Title: "Name"}, {Title: "Size", Width: 90, Align: ui.End}}
app.files.Selected = &app.file
if ui.Table(c, &app.files, cols, len(files), func(row, col int) {
	switch col {
	case 0:
		ui.Text(c, files[row].Name).SingleLine()
	case 1:
		ui.Text(c, files[row].Size())
	}
}).Grow(1).Submitted() {
	app.open(files[app.file])
}
```

### Grids

`ui.Grid` lays its children out in columns and rows, as CSS grid does:
they fill its cells row by row, and it adds rows as they need them.
`Columns(3)` makes three columns of equal width; `ColumnTracks` sets them
one by one, as `ui.Fixed(220)` DIPs, `ui.Fr(1)`, a share of the room the
others leave, or `ui.FitContent()`, as wide as their content:

```go
ui.Grid(c).Columns(3).Gap(12).Children(func() {
	for _, p := range app.photos {
		ui.Image(c, p).AspectRatio(1).Fit(ui.Cover).Radius(8)
	}
})

ui.Grid(c).ColumnTracks(ui.FitContent(), ui.Fr(1)).GapX(16).GapY(8).Children(func() {
	ui.Text(c, "Name").Bold().ColumnSpan(-1) // across every column
	ui.Text(c, "Email")
	ui.TextInput(c, &app.email)
	ui.Text(c, "Bio")
	ui.TextArea(c, &app.bio).RowSpan(2)
})
```

`ColumnSpan` and `RowSpan` make a child span tracks, a negative span every
track to the last, and `ColumnStart` and `RowStart` put it in a given
column and row, counting from 1. `RowTracks` and `Rows` set the first
rows, which `Fr` makes share the grid's height. Children stretch to fill
their cells unless `JustifyItems` (horizontally) and `AlignItems`
(vertically) say otherwise, or `JustifySelf` and `AlignSelf` for one;
automatic margins center them. Tracks that fit their content share the
room left over unless `Justify` or `AlignContent` place them instead.

## Text

`ui.Text` shows text that wraps at the width it gets, and `ui.Textf` formats
it. `FontSize`, `FontWeight`, `Bold`, `Italic`, `Font`, `LineHeight`,
`FixedLineHeight` (in DIPs, whatever the font size), `TextColor`,
`TextAlign`, `Underline`, `WavyUnderline`, `Strikethrough`,
`DecorationColor` and `DecorationThickness` (of the underline and
strikethrough), `TextBackground` (a highlight behind the lines),
`LetterSpacing` and `FontFeatures` style it, set on the text or on any
element above it, whose texts inherit them. `FontFeatures` turns on
OpenType features of the font by tag, or sets them with `tag=value`:
`FontFeatures("tnum")` gives digits of one width for numbers that change,
`FontFeatures("liga=0")` turns ligatures off. `SingleLine` keeps text on
one line, cut with an ellipsis, `MaxLines` limits it to a few, and
`Ellipsis(" →")` ends what they cut with another mark; `NoWrap` keeps its
lines whole, breaking them only at newlines. `Selectable` lets the user
select a text with the pointer, Shift and the arrows, and copy it, as an
error message or an identifier to paste elsewhere.

`ui.RichText` mixes styles in one paragraph: each `ui.Span` sets what it
changes (font, size, weight, italics, color, underlines, strikethrough,
their color and thickness, a background, letter spacing, features) over
the style of the text, and the spans wrap together:

```go
ui.RichText(c,
	ui.Span{Text: "Saved "},
	ui.Span{Text: "report.pdf", Weight: 600},
	ui.Span{Text: " to "},
	ui.Span{Text: "Documents", Color: t.Accent, Underline: true},
)

ui.RichText(c,
	ui.Span{Text: "Did you mean "},
	ui.Span{Text: "recieve", WavyUnderline: true, DecorationColor: t.Danger},
	ui.Span{Text: "? "},
	ui.Span{Text: "match", Background: ui.RGBA(250, 204, 21, 0.4)},
)
```

Text elements built in a text's `Children` continue its paragraph, as
HTML's inline elements do: each styles its own text over the paragraph's,
and keeps what elements do (`Clicked`, `Hovered`, the keyboard focus, a
`Tooltip`, a `ContextMenu`), with its words, on every line they take, as
its area. A `ui.Link` inside is a link within the sentence, which Tab
reaches and assistive technology reads as a link:

```go
ui.RichText(c).Children(func() {
	ui.Text(c, "Read ")
	ui.Link(c, "the guide", "https://example.com/guide")
	ui.Text(c, " or ")
	if ui.Text(c, "show an example").TextColor(t.Accent).Clicked() {
		app.example = true
	}
	ui.Text(c, ".")
})
```

Only text elements (`Text`, `Link`, `RichText`) go inside a text; their
sizes, padding, borders and corners do not apply, and a `Background`
highlights their text.

Text is laid out and drawn by the system's own text engine (DirectWrite on
Windows, Core Text on macOS, Pango on Linux) in the system's font (Segoe UI,
SF, the desktop's interface font, as GTK apps have it), falling back to the
system's fonts for other scripts and emoji as native apps do, with
right-to-left text in its order. `Font("monospace")` picks the system's
monospaced font, and `ui.RegisterFont` adds your own:

```go
//go:embed Inter.ttf
var inter []byte

func init() {
	if err := ui.RegisterFont(inter, "Inter"); err != nil {
		log.Fatal(err)
	}
}
```

Then `Font("Inter")` uses it, or set it for every element in the theme's
`Font`. A list of families, as `Font("Inter, Noto Sans JP")`, draws with
the first the system or the app has, and what it lacks with the next that
has it, before the system's own choice.

## Styling and themes

`Background`, `Border`, `Radius`, `Shadow` and `Opacity` style an
element's box, and `Cursor` sets the pointer over it:

- **Borders.** `Border(1, c)` draws one inside every edge; `BorderWidth`
  sets the sides apart, CSS style, with `BorderColor`, as a line under a
  header with `BorderWidth(0, 0, 1, 0)`, and `BorderStyle(ui.BorderDashed)`
  dashes it.
- **Shadows.** `Shadow(x, y, blur, spread, c)` casts a box shadow, as CSS's
  `box-shadow` does: several stack, and each shows only outside the box,
  so a translucent background never shows its own shadow through.
- **Gradients and stripes.** `Gradient(from, to, angle)` fills the box with
  a linear gradient; `LinearGradient` also places its colors along the line
  (`Start`, `End`) and mixes them in Oklab, which keeps their lightness,
  instead of sRGB. `Stripes(c, width, gap, angle)` draws stripes over the
  background, as on what is unavailable.
- **Pointer.** `Cursor` takes the shapes of the platforms: `CursorPointer`,
  `CursorText`, `CursorMove`, `CursorGrab` and `CursorGrabbing`, the
  resize cursors (both ways, toward one side as `CursorResizeE`, and of
  columns and rows), `CursorCopy` and `CursorAlias` for drops,
  `CursorContextMenu`, `CursorVerticalText`, `CursorNotAllowed`,
  `CursorCrosshair`, and `CursorNone`, which hides it.

There are no style sheets and no state selectors: the view is code, so an
element's look follows the state in the view itself, as
`if row.Hovered() { row.Background(t.SurfaceHover) }`, and one element's
state can style another, as a group's hover does in CSS.

Widgets take their colors and metrics from the theme, `c.Theme()`: the light
or the dark theme, following the system's appearance as it changes, and the
settings of the desktop that the system's own controls follow. Use its
colors in your own elements so they follow too. To change it, set a copy:

```go
t := *c.Theme() // the default, which follows the system
t.Accent, t.Radius = ui.Hex("#7c3aed"), 8
t.Spacing = 3 // compact
c.SetTheme(&t)
```

`c.Preferences()` returns those settings, which the default theme follows
and a frame follows the changes of:

- **`Accent`.** The accent color the user chose (macOS, Windows, and
  desktops whose portal gives one, as GNOME 47 and KDE do), which colors
  primary buttons, the choice and the focus ring, with text on it that
  stands out.
- **`HighContrast`.** macOS's Increase Contrast, Windows's contrast themes,
  the portal's higher contrast: borders and secondary text are darker
  (lighter in the dark), and the focus ring opaque.
- **`TextScale`.** Windows's and GNOME's text size: `FontSize` is that many
  times larger.
- **`ReduceMotion`.** macOS's Reduce Motion, Windows's animation effects and
  GNOME's animations turned off: `Animate` goes to its target at once.

`Spacing` is the unit of the room widgets leave: their paddings and gaps,
and the sizes of check boxes, switches, sliders and the rows of tables and
trees, are multiples of it. It is 4 by default; 3 makes every widget
compact, 5 roomy. `FontSize` sizes their text, `Radius` rounds their
corners, and `ScrollbarWidth` sets the width of scroll bars. Size your own
elements with the theme too, and they follow it: `t.Space(3)` is three
units of its spacing, and `t.Rem(2)` twice its font size, as CSS's rem.

A widget returns its element, so a call after it styles it differently
from the rest: `ui.Button(c, "Save").Padding(10, 20).Radius(999)`. For a
look of your own, build on the widgets' bases, which have none: see
[widgets without a look](#widgets-without-a-look).

## Widgets

| | |
|---|---|
| `Button`, `PrimaryButton` | a push button; `Clicked` reports presses by the pointer, Enter or Space |
| `MenuButton` | a button opening a menu of the system's below it (`Element.Menu`) |
| `Link` | text that opens a URL in the browser |
| `Checkbox`, `Switch` | toggle a `*bool` |
| `Radio` | sets a `*T` to its value |
| `Select` | picks one of a list of strings, from a popup |
| `Slider` | sets a `*float64` within a range, by dragging or with the arrow keys |
| `Progress` | a bar filled from 0 to 1, or sliding across for a negative value, for work of unknown length; `Reverse` fills it from the right |
| `TextInput`, `TextArea` | edit a `*string` on one line or several, with selection, undo, the clipboard and input methods; `Placeholder`, `Password`, `Submitted` (Enter) and `Changed` |
| `NumberInput` | edits a `*float64` within a range, typed or stepped with Up, Down and its buttons |
| `DateInput` | edits a `*time.Time` with a calendar, by click or with the arrow keys and Page Up and Down |
| `Tabs` | a row of tabs choosing a `*int`, by click or with the arrow keys |
| `Split`, `SplitVertical` | two panes with a divider between them that the user drags, or moves with the arrow keys, to resize them; the first's size is a `*float32` |
| `Table` | rows under a header of `TableColumn`s, as high as their tallest cell: a `List`'s rows, with its `ListState`, see [tables](#tables) |
| `Tree`, `TreeItem` | items that open and close, built inside the items they belong to, with the arrow keys moving between them; `Clicked` and `Selected` choose one |
| `Icon` | shows a `*ui.SVG` in the color of the text, as high as the font size, see [images and icons](#images-and-icons) |
| `Image` | shows a `*ui.Bitmap`, or a `*ui.SVG` in its own colors |
| `Divider`, `Spacer` | a line, and space that grows |
| `Scroll`, `ScrollHorizontal`, `ScrollBoth` | scroll containers, see [layout](#layout) |
| `List` | rows of any number and height, built only while in view, see [lists](#lists) |
| `Grid` | a grid of cells, see [grids](#grids) |
| `Modal`, `Popover`, `Overlay` | dialogs and panels above the window, see [overlays](#overlays) |

### Widgets without a look

Every widget is built on a base, the same widget without a look: the base
handles the pointer, the keyboard and the focus, and tells assistive
technology what it is, and leaves every color, size and shape to the
elements it returns, which you style as any other. Build a design of your
own on them, as headless component libraries do on the web:

| | |
|---|---|
| `ButtonBase` | a row that takes the focus, and reports `Clicked` for the pointer, Enter and Space |
| `CheckboxBase`, `SwitchBase` | a row that toggles a `*bool` |
| `RadioBase` | a row that selects its value into a `*T` |
| `SliderBase` | sets a `*float64` from where the pointer is across its content box, inside its padding, and with the arrows, Home and End |
| `TabsBase` | the tab `List`, whose `Tab`s choose a `*int`, with the arrows moving the choice and the focus |
| `SelectBase` | a `Trigger` opening a `Popup` of `Item`s choosing a `*T`, which the arrows highlight (`Highlighted`) and Enter chooses |
| `PopoverBase`, `DialogBase` | a panel below an anchor, or over a backdrop covering the window, that a click outside or Escape closes |
| `TextInputBase`, `TextAreaBase` | text inputs without padding, background, border or corners |

A segmented control on `TabsBase`, and a select on `SelectBase`:

```go
t := c.Theme()
tabs := ui.TabsBase(c, &app.view, 3)
tabs.List.Padding(3).Radius(999).Background(t.Surface).Children(func() {
	for i, name := range []string{"Day", "Week", "Month"} {
		seg := tabs.Tab(i).Padding(5, 14).Radius(999)
		if i == app.view {
			seg.Background(t.Background).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.15))
		}
		seg.Children(func() { ui.Text(c, name) })
	}
})

sel := ui.SelectBase(c, &app.size)
sel.Trigger.Gap(6).Padding(6, 10).Radius(8).Border(1, t.Border).Children(func() {
	ui.Text(c, app.size)
	ui.Icon(c, chevron)
})
sel.Popup(func(panel *ui.Element) {
	panel.Margin(4, 0, 0, 0).Padding(4).Radius(10).Background(t.Background).Border(1, t.Border)
	for _, size := range sizes {
		item := sel.Item(size).Padding(6, 10).Radius(6)
		if item.Highlighted() {
			item.Background(t.Accent).TextColor(t.AccentText)
		}
		item.Children(func() { ui.Text(c, size) })
	}
})
```

Bases ring the element with the keyboard focus, as every element taking
the focus is; `FocusRing(false)` turns the ring off for a widget that
draws its own, with `Painter.FocusRing` while `FocusVisible`.

Widgets that change a value take a pointer to it, so they need no handler:
`ui.Checkbox(c, &app.settings.Sync, "Sync")` changes the field the moment
the user clicks. `Changed` reports that they did, for work that follows:

```go
if ui.TextInput(c, &app.query).Placeholder("Search").Changed() {
	app.results = search(app.query)
}
```

Build your own widgets from elements. An element keeps state of its own
from frame to frame with `ui.Local`:

```go
func Disclosure(c *ui.Context, title string, body func()) {
	box := ui.Column(c)
	open := ui.Local(box, "open", func() bool { return false })
	box.Children(func() {
		head := ui.Row(c).Gap(6).Cursor(ui.CursorPointer).Focusable()
		if head.Clicked() {
			*open = !*open
		}
		head.Children(func() {
			ui.Text(c, map[bool]string{true: "▾", false: "▸"}[*open])
			ui.Text(c, title).Bold()
		})
		if *open {
			body()
		}
	})
}
```

## Input

- **Pointer.** `Hovered`, `Pressed`, `Clicked`, `DoubleClicked`,
  `RightClicked`, `Dragged` (how far the pointer moved since the last frame
  while pressing the element) and `PointerPosition`. `ClickModifiers`
  returns the modifier keys held for the last click, as Shift for a
  Shift-click. `PassThrough` lets the pointer through to what is below.
- **Keyboard focus.** `Focusable` elements take the focus when clicked, and
  Tab and Shift+Tab move it between them, with a focus ring when it moves
  by keyboard. `AutoFocus` gives an element the focus when it appears, such
  as the first field of a dialog, and `Focus` keeps it there while you call
  it; `Focused`, `FocusVisible` and `FocusWithin` report it. Enter and Space
  press a focused button or link, and Space toggles a focused check box,
  switch or radio button.
- **Shortcuts.** `c.Shortcut(ui.Cmd, ui.KeyS)` reports a key pressed with
  exactly those modifiers anywhere in the window, and `Element.Shortcut`
  only while the element or one inside it has the focus, which comes first.
  A focused button or link keeps Enter and Space, and a toggle Space, so
  `c.Shortcut(0, ui.KeyEnter)` presses a dialog's default button wherever
  else the focus is.
  `ui.Cmd` is Command on macOS and Ctrl elsewhere. Shortcuts of
  [menus](menus.md) still work, and the Edit menu's roles (cut, copy, paste,
  select all, undo, redo) act on the focused text input. A focused text
  input takes the editing keys of the platform first: on macOS, Option and
  Command with the arrows and Backspace, and Control with A, E, B, F, N, P,
  D, H and K, as in other Mac apps.
- **Input methods.** Text inputs take text composed with input methods,
  which see the text around the caret: macOS's press and hold replaces the
  letter it accents, Japanese input methods convert typed text again, and
  others predict from what comes before.
- **Files.** `DroppedFiles` returns the paths of the files dropped on the
  element from Finder, Explorer or a file manager, and `FileDragOver`
  reports files dragged over it, to show it would take them. The window
  takes files only over such elements, or anywhere when it has
  `OnFileDrop` listeners, which get the files no element takes, with where
  they were dropped:

  ```go
  zone := ui.Column(c).Size(260, 80).Border(1, t.Border)
  if files := zone.DroppedFiles(); files != nil {
  	app.files = files
  }
  if zone.FileDragOver() {
  	zone.Border(2, t.Accent)
  }
  ```
- **Context menus.** `ContextMenu` gives an element a menu of the system's,
  which opens where the element is right-clicked (Control-clicked on
  macOS), and below it when the menu key or Shift+F10 is pressed while it
  or one inside it has the focus. The function builds the items when the
  menu opens, and runs again in the frame after one was chosen, where its
  `Chosen` reports it:

  ```go
  row.ContextMenu(func(m *ui.Menu) {
  	if m.Item("Rename").Shortcut(0, ui.KeyF2).Chosen() {
  		app.renaming = i
  	}
  	if m.Item("Pinned").Checked(note.pinned).Chosen() {
  		note.pinned = !note.pinned
  	}
  	m.Submenu("Move to", func(m *ui.Menu) {
  		for _, f := range app.folders {
  			if m.Item(f.name).Chosen() {
  				app.move(i, f)
  			}
  		}
  	})
  	m.Separator()
  	if m.Item("Delete").Disabled(note.locked).Chosen() {
  		app.delete(i)
  	}
  })
  ```

  The innermost element with a menu gets the click. Text inputs have the
  editing commands of their platform's text fields, unless `ContextMenu`
  gives them another menu. `Shortcut` only shows a key: handle it with
  `Shortcut` on the context or an element.
- **Menu buttons.** `Element.Menu` builds the same menu for a button,
  which opens below it as the pointer goes down on it, as the system's
  pop-up buttons do, and for Enter, Space or Down while it has the focus;
  `MenuButton` is a button with a label and an arrow that does:

  ```go
  ui.MenuButton(c, "Sort by", func(m *ui.Menu) {
  	for _, by := range []string{"Name", "Date", "Size"} {
  		if m.Item(by).Checked(app.sort == by).Chosen() {
  			app.sort = by
  		}
  	}
  })
  ```

  Assistive technology sees a menu button (`AXMenuButton` on macOS, a push
  button menu on Linux, a button that expands on Windows).
- **Every key, as it comes.** Widgets that take every key themselves, as
  the [terminal](plugins/terminal.md) does, get their input as it comes
  with `HandleInput`, before the next frame: keys pressed and released,
  text typed and composed while they have the focus, the edit commands of
  the menus, and the pointer pressed on them, moving over them or
  scrolling over them. The function reports whether it took the event; one
  it leaves goes on to shortcuts, Tab, context menus and scroll containers
  as usual, and keys that the window or an element around the focus
  handles with `Shortcut` go there first. `TextCaret` turns on the system's
  input methods for such an element while it has the focus, composing at
  the caret it gives. `c.ReadClipboard`, `c.WriteClipboard` and
  `c.OpenURL` copy, paste and open links for them:

  ```go
  ui.Box(c).Fill().Focusable().HandleInput(func(ev ui.InputEvent) bool {
  	switch ev.Kind {
  	case ui.InputKeyDown:
  		return app.key(ev.Mods, ev.Key)
  	case ui.InputText:
  		app.insert(ev.Text)
  		return true
  	}
  	return false
  }).TextCaret(app.caretRect())
  ```
- **Tooltips.** `Tooltip("…")` shows a tip once the pointer rests on the
  element.
- **Custom title bars.** In a `Frameless` window, `DragWindow` makes an
  element move the window, and a double click on it maximizes the window.

## Overlays

`ui.Modal` shows a dialog over a dimmed window while a `*bool` is true, and
`ui.Popover` a panel below an element, such as a menu; clicking outside them
or pressing Escape sets it to false:

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

A dialog keeps the keyboard: it takes the focus as it opens, on its first
element that takes it unless one in it asked for it (`AutoFocus`), Tab
goes round its elements, and the window's shortcuts built outside it
wait, as what is behind it is inert, which screen readers do not see
either. A popover's elements follow its anchor as Tab moves. Escape closes
the overlay on top, a select's popup before the dialog it is in, unless
the focused element takes it, as a terminal does; and an overlay that
closes with the focus in it gives the focus back to the element that had
it as it opened, its button say.

`c.Toast("Saved")` shows a message near the bottom of the window for a few
seconds, as the outcome of what the user just did; screen readers see it
as a status. `ui.Overlay` builds elements above everything else, placed
with `Absolute` in DIPs of the window. Native [dialogs](native.md#dialogs) work too: call
them from a goroutine, so that the view does not wait for them.

## Accessibility

Assistive technology, such as VoiceOver on macOS, Orca on Linux and
Narrator or NVDA on Windows, reads a window's native UI as it reads other
apps: the widgets with their roles, names, values and states, the texts,
and the elements that take the focus, as the content changes. It acts on
them as the keyboard and the pointer would: it presses buttons, checks
boxes, moves sliders, edits text inputs and moves the focus.

Widgets describe themselves, and the text inside an element names it, as
a button's does. Elements without text need a `Label`, which also finds
them in [tests](#testing):

```go
ui.Slider(c, &app.volume, 0, 100).Label("Volume")
ui.TextInput(c, &app.query).Placeholder("Search").Label("Search")
ui.Box(c).Size(24, 24).Draw(drawIcon).Label("Unread messages")
```

Other elements get a role from what they do: one that is clickable and
takes the focus is a button, a scroll container a scroll area, and one
with a `Label`, or that takes the focus, a group. `Role` sets it for an
element drawn as a widget it is not built from, and `ui.RoleNone` leaves
an element out but not its children:

```go
toggle := ui.Box(c).Size(36, 20).Focusable().Role(ui.RoleSwitch).Label("Wi-Fi")
```

A `List`'s rows are list items, and a `Table`'s rows, named by the text
inside them, and each says which of all the rows it is, "5 of 10,000",
although the list builds only those in view. A list choosing its rows
gives assistive technology the focus on the row chosen: Up and Down are
read as they move the choice, and focusing a row chooses it. A screen
reader moving out of view asks the list to scroll there, which builds the
rows it reaches. `Label` names a list or a table.

MyGo describes frames only once assistive technology asked, so apps pay
nothing for it otherwise.

## Drawing and animation

`Draw` paints on an element after its background, and `DrawOver` after its
children, with a `*ui.Painter` in DIPs of the window: rectangles with
`Fill`, `FillGradient`, `Stroke` and `StrokeDashed`, `Shadow`, `Line`,
`Image`, `Clip`, paths of lines and curves with `FillPath` and
`StrokePath`, or in a gradient with `FillPathGradient` and
`StrokePathGradient`, and text with `Text`, or `RichText` in spans of any
style, which `MeasureText` measures first, to center or align it:

```go
ui.Box(c).Height(120).Draw(func(p *ui.Painter, r ui.Rect) {
	var wave ui.Path
	for i := 0; i <= 100; i++ {
		x := r.X + r.W*float32(i)/100
		y := r.Y + r.H/2 + 40*float32(math.Sin(float64(i)/8))
		if i == 0 {
			wave.MoveTo(x, y)
		} else {
			wave.LineTo(x, y)
		}
	}
	p.StrokePath(&wave, 2, c.Theme().Accent)
})
```

```go
ui.Box(c).Size(200, 40).Draw(func(p *ui.Painter, r ui.Rect) {
	label := ui.Span{Text: "42%", Weight: 600, Color: c.Theme().TextMuted}
	w, h := p.MeasureText(0, label)
	p.RichText(r.X+(r.W-w)/2, r.Y+(r.H-h)/2, 0, label)
})
```

Draw functions only paint: MyGo may call them more than once a frame.

Text that a widget lays out itself, as in the cells of a grid, is shaped
once with `ui.Shape`, which returns glyphs placed along a line by the
system's text engine, with ligatures, kerning and fallback fonts, and the
runes each comes from. Move their `X` and draw them with `Painter.Glyphs`;
`Font.Metrics` returns the font's ascent, descent and line gap, and
`Painter.Scale` the device pixels of a DIP, to line things up with the
display's pixels. `Shape` caches nothing, unlike the text of elements:
keep the glyphs of text drawn in many frames. `Font.Features` turns
OpenType features on and off, and `Font.Thicken` draws text with a
thicker stroke on macOS, as Ghostty's `font-thicken`.

```go
font := ui.Font{Family: "monospace", Size: 13}
glyphs := ui.Shape("grid", font)
for i := range glyphs {
	glyphs[i].X = float32(glyphs[i].Cluster) * cellWidth // one per cell
}
ui.Box(c).Height(20).Draw(func(p *ui.Painter, r ui.Rect) {
	p.Glyphs(glyphs, r.X, r.Y+font.Metrics().Ascent, c.Theme().Text)
})
```

For motion, `Element.Animate` returns a value that eases to a target and
draws frames until it gets there:

```go
panel := ui.Column(c).Clip()
width := float32(0)
if app.sidebar {
	width = 280
}
panel.Width(panel.Animate("width", width, 200*time.Millisecond))
```

`AnimateWith` takes the easing: `ui.Linear`, `ui.EaseIn`, `ui.EaseOut` (as
`Animate`), `ui.EaseInOut`, any `func(t float32) float32`, or
`ui.Bounce(e)`, which goes along `e` and comes back. `Loop` returns the
progress of an animation that starts over every period, for spinners and
pulses; `Rotate` turns an icon:

```go
spin := ui.Icon(c, loader)
spin.Rotate(spin.Loop("spin", time.Second, ui.Linear) * 360)

skeleton := ui.Box(c).Height(14).Radius(7).Background(t.Border)
skeleton.Opacity(0.4 + 0.6*skeleton.Loop("pulse", 1600*time.Millisecond, ui.Bounce(ui.EaseInOut)))
```

To animate in other ways, compute from `c.Now()` and call
`c.AnimationFrame()` in every frame that moves: MyGo draws the next frame
when the display can show it, and draws nothing while nothing changes.

When the desktop asks for less motion (`c.Preferences().ReduceMotion`),
`Animate` and `AnimateWith` go to their target at once. `Loop` goes on, as
the system's spinners do: it shows that something is going on. Motion you
compute yourself should read the preference too.

## Images and icons

`ui.NewBitmap` makes a bitmap of an `image.Image`, and `ui.DecodeBitmap` of
PNG, JPEG, GIF (its first frame), WebP or BMP data, turning photos upright as
their camera's EXIF orientation says. Make bitmaps once, not in the view:
MyGo keeps a bitmap on the GPU as long as you use it. A bitmap shown much
smaller than its pixels, as a photo in a thumbnail, is drawn from a copy
halved as many times as that keeps it no smaller, made once, so that it
shows every pixel's part rather than shimmering.

```go
//go:embed logo.png
var logoPNG []byte

var logo, _ = ui.DecodeBitmap(logoPNG)

ui.Image(c, logo).Size(64, 64).Fit(ui.Contain).Radius(12)
```

`Fit` says how an image fills its box: `Contain` (the default) fits it
inside, `Cover` covers the box and crops the rest, `FillBox` stretches it,
`ScaleDown` shows it at its own size unless that does not fit, and
`NaturalSize` at its own size, cropped. `Grayscale` draws it in shades of
gray, as for what is disabled.

SVG files stay sharp at any size and scale. `ui.ParseSVG` parses one, and
`ui.MustParseSVG` one that is part of the program, such as an embedded
file, panicking when it is in error. `ui.Icon` shows an SVG as an icon: in
the color of the text, which it takes from its ancestors as text does,
whatever colors the file has, and as high as the font size. Icon sets such
as Lucide, Heroicons, Tabler or Material Symbols work as they are:

```go
//go:embed icons/save.svg
var saveSVG []byte

var save = ui.MustParseSVG(saveSVG)

ui.Row(c).Gap(6).Children(func() {
	ui.Icon(c, save)
	ui.Text(c, "Save")
})
ui.Icon(c, save).FontSize(24).TextColor(c.Theme().Danger)
```

Inside a button, which lays out its children in a row and gives them its
text color, an icon goes before the label:

```go
ui.PrimaryButton(c, "").Children(func() {
	ui.Icon(c, save)
	ui.Text(c, "Save").SingleLine()
})
```

`ui.Image` shows an SVG in its own colors instead, with the text color for
its `currentColor`, at the size the SVG gives (its `width` and `height`, or
those of its `viewBox`) unless given another:

```go
ui.Image(c, illustration).Width(240)
```

MyGo draws an SVG's shapes into the GPU's atlas once for each size it shows
at, the way it draws text, and the GPU draws them from there: moving an
icon, scrolling it or changing its color draws nothing again. MyGo draws
paths and the basic shapes, groups, `use` and `symbol` elements,
transforms, fills and strokes (with their joins, caps and dashes) of
colors, `currentColor` and linear and radial gradients, opacity, clip paths,
masks and style sheets of simple selectors (type, class and id). It leaves
out text, embedded images, patterns, markers and filters: convert text to
paths in your editor before exporting.

Icons are decorations that assistive technology does not see, as images
are, unless `Label` names them. In `Draw` callbacks, `Painter.Icon` and
`Painter.Image` draw SVGs too.

## Windows with native UI

A window with `Content` takes the [window options](windows.md#options) of
any window, such as its size, `StateKey`, `Frameless` and `Parent`, as well
as menus, dialogs and the other [desktop APIs](native.md). It has no page:
`Page()` is nil, and it ignores `URL` and `WindowOptions.Page`. Its Go code
needs no bindings: the view calls it directly. `CapturePage` returns a PNG
of what it shows.

With `TitleBarStyle: mygo.TitleBarHidden`, the view draws the title bar
under the window controls, as a page does with the `--mygo-titlebar-*` CSS
variables: `c.TitleBar()` returns the room the controls take, zero in full
screen, and `DragWindow` makes elements drag the window, which
double-clicking them zooms or minimizes as a title bar would:

```go
bar := c.TitleBar()
ui.Row(c).Height(max(bar.Height, 32)).Padding(0, bar.Right+12, 0, bar.Left+12).DragWindow().Children(func() {
	ui.Text(c, "Inbox").Bold()
})
```

On macOS, a window's `Vibrancy` shows wherever its native UI draws no
background. The root draws the theme's by default: make it transparent and
give backgrounds to the parts that need one, as a sidebar beside opaque
content does:

```go
c.Root().Background(ui.Transparent)
ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
	app.sidebar(c) // over the material
	ui.Column(c).Grow(1).Background(c.Theme().Background).Children(func() { app.content(c) })
})
```

An app whose windows all show native UI needs no webview: on Linux it
needs GTK 3 alone, not WebKitGTK, and on Windows no WebView2 Runtime.

## Testing

`ui.NewTester` runs a view without a window, as fast as a unit test: it
renders frames in memory, finds elements by their text, and clicks, types,
scrolls and presses keys.

```go
func TestCounter(t *testing.T) {
	s := &counter{}
	tt := ui.NewTester(s.view, 320, 240)
	if err := tt.Click("Increment"); err != nil {
		t.Fatal(err)
	}
	if s.n != 1 || !tt.HasText("1") {
		t.Errorf("count %d, texts %q", s.n, tt.Texts())
	}
}
```

`tt.SetPreferences` changes the desktop's preferences, as `tt.SetDark` its
appearance. `tt.RightClick` opens a context menu, which `tt.Menu` lists and
`tt.ChooseMenuItem("Move to", "Archive")` chooses from. `tt.TypeKey` presses
a key with the text it types, `tt.SetFocused` takes the keyboard from the
window and gives it back, `tt.Compose` shows the composition of an input
method, `tt.Command` performs an edit command
of the menus, such as `"copy"`, and `tt.TextCaret` returns where input
methods would compose. `tt.Image()` is the
last frame, for snapshots, and `ui.Render` draws a view once at a given
scale.

## Rendering

MyGo draws on the GPU with Metal on macOS, with Direct3D 11 on Windows,
or with WARP, Windows' own software renderer, where no GPU driver works,
and with OpenGL on Linux, in the GtkGLArea GTK shows. A shader computes
rounded rectangles, borders, gradients and shadows from the distance to
their edges, so they stay sharp at any size and scale, and text comes
from a glyph atlas that only uploads what changes.

On macOS, frames that change little, such as a clock ticking, typing or
the pointer over a button, are drawn on the CPU, which redraws only what
changed, and go to the screen without waking the GPU: they take less
time than the GPU takes to start, and spare the memory Metal's driver
holds for a couple of seconds after each frame it draws. Scrolling,
resizing and animations of much of the window use the GPU.

Where OpenGL would not run on a GPU, as in virtual machines or in WSL
(where `GALLIUM_DRIVER=d3d12` gives Mesa the GPU), Linux draws the same
pixels on the CPU: a few milliseconds for a whole large window on a
high-density display, and less than a tenth of one for what typically
changes, such as a button under the pointer, since it redraws only that.
Set `MYGO_GPU=0` to use the CPU renderer everywhere, for instance to
compare, and on Linux `MYGO_GPU=1` to draw with OpenGL even where it runs
on the CPU.
