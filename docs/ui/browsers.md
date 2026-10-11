# Native UI in browsers

An app of native UI also runs in a browser, compiled to WebAssembly: its
windows show on canvases of a page, drawn with WebGL 2 by the same
renderer as on Linux, with the same shaders. Nothing changes in the app:

```go
func main() {
	s := &counter{}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "Counter", Content: ui.View(s.view)})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
```

Build it for `js/wasm`, with Go's loader of WebAssembly beside it:

```sh
GOOS=js GOARCH=wasm go build -o app.wasm .
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" .
```

and load it from a page:

```html
<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <style>html, body { margin: 0; height: 100%; }</style>
    <script src="wasm_exec.js"></script>
    <script>
      const go = new Go();
      WebAssembly.instantiateStreaming(fetch("app.wasm"), go.importObject).then((r) => go.run(r.instance));
    </script>
  </head>
  <body></body>
</html>
```

Serve the directory over HTTP, with WebAssembly as `application/wasm`, as
most servers do. A window fills the element with the ID `mygo`, or else
the page; a window opened later covers the earlier ones. The page sets the
window's size: the size, position and title bar options do nothing, and
the title of the window opened last is the page's.

## What works

- **Drawing.** Frames draw with WebGL 2 at the device pixel ratio, once
  for each of the browser's frames that something changed in; effects, as
  [Liquid Glass](../plugins/glass.md), draw as on the desktop. Where the
  browser has no WebGL 2, frames draw in memory and show through a 2D
  canvas. A lost WebGL context draws again once the browser restores it.
- **Text** comes from the browser, which finds the fonts, falls back for
  what one lacks and shapes, as for its pages: `system-ui` is the system's
  font, as a page's. Lines break and runs order (bidi) as in package text
  everywhere. Text is not drawn with subpixel antialiasing.
- **Input**: the pointer, with the clicks counted; the wheel and touchpads;
  keys; input methods, which compose in a text area out of sight at the
  caret; and the clipboard's text, through the browser's copy, cut and
  paste. On a Mac, ⌘ is Control for the view's shortcuts, which take a
  browser for a system other than macOS.
- **Routers.** The first [router](navigation.md) a window shows follows
  the tab's history, in the URL's fragment, as `#/notes/42`: the browser's
  back and forward buttons go through its pages, and a link to a page, or
  a reload, opens the app there.
- **Context menus** show as menus of HTML on the page.
- **Dark mode**, reduced motion and more contrast follow the browser's
  settings, and change with them.
- `Dialog.Message` asks with the browser's `alert` and `confirm`, and
  `Shell.OpenExternal` opens a tab.

## What does not

Windows of web pages, application and Dock menus, trays, notifications,
global shortcuts, file dialogs and the drag and drop of data have nothing
in a browser to stand for them, and report so. Assistive technology does
not see the content yet. Of a style's OpenType features, canvases apply
`kern` only. A font added with `ui.RegisterFont` needs its family, and
draws once the browser loaded it. The clipboard's text comes from the
system when a paste brings it: the view's own Paste command pastes what
the app copied last.

## Size

The counter example is 19 MB of WebAssembly, 4.5 MB compressed with gzip.
Most of it is package `mygo`'s for desktops (its HTTP server, cryptography
and the TypeScript generator), which the browser does not use; the toolkit
alone, with the gallery's views, is 2.7 MB compressed. Building for
browsers adds nothing to apps for desktops: the browser's code is only in
`js/wasm` builds.

## Under the hood

Go's main goroutine runs the event loop; the browser's events reach it
through the functions JavaScript calls, which wait for it to handle them,
so that they can still prevent the browser's defaults. The architecture
guide's [Browsers](../architecture.md#browsers-internalweb) section has
the details.
