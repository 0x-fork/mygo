# The frontend

An app's user interface is a web frontend: HTML, CSS and JavaScript built
with any tools. New projects use TypeScript and [Vite](https://vite.dev), but
MyGo only needs a URL to load, during development, and a directory of built
files, for builds.

## How pages load

Windows load pages of the frontend with URLs without a scheme, such as `/`
or `/settings?tab=general`:

```go
mygo.NewWindow(mygo.WindowOptions{URL: "/"})
win.LoadURL("/settings")
```

They resolve against:

- during `mygo dev`, the dev server at `devUrl` in mygo.json, which
  `devCommand` starts, e.g. `http://localhost:5173/`, so the dev server's
  hot reload works in the app;
- in builds, `mygo://localhost/`, which serves the files of `frontendDist`
  that `mygo build` embeds into the executable after running
  `buildCommand`.

```json
{
  "devUrl": "http://localhost:5173",
  "devCommand": "bun run dev:web",
  "buildCommand": "bun run build:web",
  "frontendDist": "dist"
}
```

Without `devUrl`, `mygo dev` serves `frontendDist` from disk instead. Paths
without a file extension that match no file serve `index.html`, so
client-side routers work.

The frontend is a secure context, and its origin is `mygo://localhost` on
macOS and Linux and `https://mygo.localhost` on Windows, where WebView2
serves custom schemes under `https://<scheme>.localhost`. Storage such as
`localStorage` and IndexedDB belongs to an origin, so data a page stores
under `mygo dev` (the dev server's origin) is not what the built app sees.
Keep data you care about in Go, for example in a file in
`App.Path(mygo.PathUserData)`, and hand it to pages through bound methods.

Windows can also load other content:

- `win.LoadURL("https://example.com")`, a web page;
- `win.LoadHTML(html, baseURL)`, an HTML string;
- `win.LoadFile("docs/index.html")`, a local file, resolved against the
  working directory, then the executable's directory (and the Resources
  directory of a macOS app).

Pages that are not the app's own cannot call Go methods: see
[who may call](bindings.md#who-may-call).

### Without the CLI

`mygo build` embeds the frontend by calling `mygo.SetFrontend`, and a
program built with `go build` can do the same with `embed`:

```go
//go:embed all:dist
var dist embed.FS

func main() {
	web, _ := fs.Sub(dist, "dist")
	mygo.SetFrontend(web)
	// ...
}
```

## Custom protocols

`mygo.Protocol.Handle` serves a URL scheme from an `http.Handler`, for
content your app produces or reads at run time: pages, images, files of
the user. Handlers run on their own goroutines and stream their responses,
and pages load such URLs like web URLs, with `fetch`, `<img>`, ES modules
and relative URLs:

```go
// Thumbnails generated into the cache directory.
cache, _ := mygo.App.Path(mygo.PathCache)
mygo.Protocol.Handle("thumbs", mygo.FileServer(os.DirFS(cache)))

// Anything an http.Handler does.
mygo.Protocol.HandleFunc("api", func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"path": r.URL.Path})
})
```

```html
<img src="thumbs://localhost/photo-42.png" />
```

Register schemes before creating the windows that use them.
`mygo.FileServer` serves an `fs.FS` with content types and range requests,
falling back to `index.html` like the frontend. Handling the `mygo` scheme
replaces the frontend with your handler. Pages of custom schemes are the
app's own: they may call Go methods.

## The mygo-runtime package

MyGo injects its runtime into every page as `window.mygo`. The
`mygo-runtime` npm package, which new projects depend on, gives it types
and imports; the generated client is built on it.

```ts
import { currentWindow, isMyGo, onFileDrop, runtime } from "mygo-runtime";

if (isMyGo() && runtime().platform === "darwin") {
  document.documentElement.classList.add("mac");
}

await currentWindow.toggleMaximize();
```

| Export | What it does |
|---|---|
| `call(method, ...args)`, `on(event, listener)`, `once(event, listener)`, `event<T>(name)` | untyped calls and events, see [bindings](bindings.md#without-the-generated-client) |
| `isCallError(err)` | whether a rejection came from a Go error |
| `isMyGo()` | whether the page runs in a MyGo window, not in a browser tab of the dev server |
| `runtime()` | the runtime: `platform` (`"darwin"`, `"linux"` or `"win32"`), `windowId` (the Go window's `ID()`) and `version`; throws outside MyGo |
| `currentWindow` | the page's window: `minimize`, `maximize`, `unmaximize`, `toggleMaximize`, `isMaximized`, `toggleFullScreen`, `close` and `setTitle` |
| `onFileDrop(listener)` | files dropped on the window, with their paths |

Opened in a regular browser, which is handy for working on the layout,
pages have no runtime: calls reject and `isMyGo()` is false.

## Custom title bars

A frameless window has no title bar or borders, and the page draws its own.
Mark the elements that drag the window with the CSS property
`--app-region: drag`:

```go
mygo.NewWindow(mygo.WindowOptions{URL: "/", Frameless: true})
```

```css
.titlebar {
  --app-region: drag;
  height: 40px;
}
.titlebar .search {
  --app-region: no-drag;
}
```

```ts
import { currentWindow } from "mygo-runtime";

minimizeButton.onclick = () => currentWindow.minimize();
maximizeButton.onclick = () => currentWindow.toggleMaximize();
closeButton.onclick = () => currentWindow.close();
```

The property is inherited: `no-drag` excludes parts of a region again, and
buttons, links, inputs and other interactive elements inside a region stay
clickable. Double-clicking a region does what double-clicking a title bar
does.

On macOS a window can keep its traffic lights and only hide the title bar,
so the page extends under it: set `TitleBarStyle` to `mygo.TitleBarHidden`,
or `mygo.TitleBarHiddenInset` for more room around the buttons, and move
them with `TrafficLightPosition`.

`Transparent: true` lets a page with a transparent background show the
desktop through, and `Vibrancy` puts a blurred material behind it on macOS
and Windows 11:

```go
mygo.NewWindow(mygo.WindowOptions{
	URL:           "/",
	TitleBarStyle: mygo.TitleBarHidden,
	Transparent:   true,
	Vibrancy:      mygo.VibrancySidebar, // Mica on Windows 11
})
```

See `examples/frameless` for a complete custom title bar.

## Dropped files

Files dragged from Finder or Explorer and dropped on the page reach the
page with their paths, which DOM drop events do not tell:

```ts
import { onFileDrop } from "mygo-runtime";

onFileDrop(({ paths, x, y }) => {
  const target = document.elementFromPoint(x, y);
  // ...
});
```

and Go, with `win.OnFileDrop`. The page's own drag and drop keeps working:
its drop listeners still get the `File` objects, and dropping files where
the page does not handle them no longer replaces the page with the file.

## Dark mode

Pages follow the system appearance through the `prefers-color-scheme` media
query. Declare `color-scheme: light dark` in CSS so that form controls and
scrollbars follow too, and give windows a `BackgroundColor` matching the
page, which the window shows until the page paints:

```css
:root {
  color-scheme: light dark;
}
```

`mygo.Theme.SetSource(mygo.ThemeDark)` forces an appearance, which pages see
through the same media query.

## Links and new windows

Links with `target="_blank"` and `window.open()` call the window's open
handler. Without one, `http(s)` URLs open in the default browser and
anything else is denied; `win.SetWindowOpenHandler` decides otherwise:

```go
win.SetWindowOpenHandler(func(req mygo.WindowOpenRequest) *mygo.WindowOptions {
	if strings.HasPrefix(req.URL, "https://docs.example.com/") {
		return &mygo.WindowOptions{Width: 900, Height: 700} // a window of the app
	}
	go mygo.Shell.OpenExternal(req.URL) // the default browser
	return nil
})
```

`win.OnWillNavigate` can cancel the page's own navigations, e.g. to keep the
app's window on the app: see [Windows](windows.md#pages).

## Preload scripts

`WindowOptions.PreloadScript` runs JavaScript in every page of the window
before the page's own scripts, once `window.mygo` exists.

## The web inspector

Development builds have the web inspector: right-click a page and choose
Inspect Element (Inspect on Windows), or call `win.OpenDevTools()`.
Production builds do not, unless built with `mygo build -debug` or a window
sets `DevTools: mygo.DevToolsEnabled`.
