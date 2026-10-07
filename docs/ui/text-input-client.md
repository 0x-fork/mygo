# Text-input primitives

`TextInputBase` and `TextAreaBase` provide ordinary editable string controls
without a look. For a control with its own storage, selections, rendering or
editing policy, attach a `TextInputClient` to an element:

```go
ui.Box(c).
    HandleTextInput(app.input).
    HandleInput(app.input.HandleInput).
    Draw(app.input.Draw).
    Label("Document").
    Role(ui.RoleTextField)
```

The primitive connects your state to the platform's input methods and text
queries. Your client owns its buffer, selection model, marked text and undo
history. It can use a rope, persistent tree or incremental paragraph store;
MyGo does not copy a whole document on each edit or choose formatting behavior.
The existing `HandleInput`, `TextCaret`, `TextInput` and `TextArea` APIs remain
available. `TextCaret` is useful for terminals and other widgets that need a
candidate-window location but do not provide a text document.

Run `go run ./examples/text-input` for a small application-owned text field.
It demonstrates composition, pointer selection, Edit-menu commands and custom
rendering. It is a primitive example, not an editor implementation.

## Client contract

Pass a pointer to a client kept across frames. `HandleTextInput(nil)` (including
a typed nil pointer) disconnects it. The element is focusable and has the text
cursor; its size, appearance and other input behavior are yours.

| Method | Purpose |
|---|---|
| `TextForRange(range)` | Return requested text and the actual clamped/adjusted range. Full-document queries are supported; no fixed surrounding-window restriction applies. |
| `Selection()` | Report the primary native selection and its direction. Additional cursors or discontiguous ranges stay in application state. |
| `MarkedRange()` | Report preedit in the current document, or false when none exists. |
| `ReplaceText(range, text)` | Commit text in an explicit range; a nil range means preedit if present, otherwise the selection. Clear preedit according to your editing policy. |
| `SetMarkedText(range, text, selected)` | Replace preedit, retaining its marked range. `selected` is relative to the newly marked text. |
| `UnmarkText()` | Clear marked status while retaining document text. Also called when focus is lost, the client changes, or the element is disposed. |
| `BoundsForRange(range)` | Return element-relative text/caret geometry, the actual range represented, and whether it is available. Return false for unavailable/offscreen text. |
| `IndexForPoint(point)` | Hit-test an element-relative point and return a document offset, or false. |

Ranges count **UTF-16 code units**, matching native text services and GPUI's
input-handler contract. This is an interop coordinate system, not a storage
format. `ui.UTF16Len` and `ui.UTF16ByteOffset` help simple UTF-8 buffers convert
at the boundary; an indexed buffer can provide faster conversions itself.
A surrogate pair must stay whole, and your editing policy should respect
Unicode grapheme boundaries. `TextForRange` returns its adjusted range so the
platform knows what text it received.

Callbacks run synchronously on the UI thread, before the next frame. They must
not wait for work requiring that thread. The client owns synchronization with
other goroutines; schedule application state changes with `Window.Update` as
for other native UI views. After a native mutation, MyGo asks for a frame and
reads the latest selection/caret before the next input event. Stale native
references cannot modify an unfocused, replaced or disposed client.

On macOS the client supplies `NSTextInputClient` selection, marked ranges,
substring queries, range bounds and point hit testing. GTK uses bounded
surrounding text, preserves UTF-8/rune/UTF-16 conversions, and forwards
surrounding deletions and preedit/commit callbacks. Windows IMM32 uses bounded
document-feed/reconversion context and forwards composition and commit ranges.
Changing the client resets native preedit so a candidate cannot move to a
newly focused field. The platform callbacks are allocated once at startup.

Keyboard navigation, pointer selection, clipboard policy and Edit-menu
commands come through `HandleInput`. They belong to your control. This permits
an editor to choose multiple selections, custom keymaps, grouping and history
without overriding behavior built into a framework-owned editor.

## Retained text geometry

`ShapeText(text, font, width)` and `ShapeRichText(spans, font, width)` produce a
`TextLayout` using Core Text, Pango or DirectWrite. Positive `width` wraps text;
zero only breaks at newlines. Cache layouts by paragraph or line and replace
only those affected by an edit.

```go
layout := ui.ShapeText(paragraph, ui.Font{Size: 16}, availableWidth)
caret := layout.Caret(offset)
offset = layout.IndexAt(ui.Point{X: pointerX, Y: pointerY})
rects := layout.SelectionRects(selectedRanges...)
// Inside Draw:
p.TextLayout(layout, originX, originY, color)
```

Layout offsets are UTF-16 too. `Caret` and `IndexAt` snap to whole graphemes;
`PreviousBoundary` and `NextBoundary` support logical navigation/deletion.
`SelectionRects` accepts multiple ranges and keeps the separate visual pieces
of bidi selections. It supplies geometry without inventing a single-range
selection model or choosing an editor's navigation policy. Size and geometry
queries are safe from any goroutine; painting is part of a UI frame.
