# MyGo architecture

This guide explains how MyGo is put together: the layers, the threading
model, how Go talks to the native toolkits without cgo, how the page and Go
exchange typed messages, and how to extend the framework safely. Read it
before changing anything under `internal/`.

## Goals and constraints

- **Low overhead.** A hello-world app is a ~7 MB binary with a ~35 MB
  physical footprint on macOS (mostly AppKit/WebKit), ~62 MB with the
  processes WKWebView runs for its page, GPU and network, and idles at 0%
  CPU. Nothing polls: all work is driven by native events or explicit
  wake-ups.
- **No cgo.** Everything builds with `CGO_ENABLED=0`, so any platform can be
  cross-compiled from any machine. Native APIs are called at run time through
  [purego](https://github.com/ebitengine/purego) (`dlopen` + assembly
  trampolines) on macOS and Linux and through the `syscall` package on
  Windows, never through `import "C"`.
- **The system webview.** WKWebView on macOS, WebKitGTK 4.1 (4.0 as a
  fallback) on Linux, WebView2 on Windows (amd64 and arm64). No browser engine
  is bundled. The bundled CEF option is planned but not implemented; on
  unsupported platforms the `internal/unsupported` backend makes `App.Run`
  fail with a clear error while everything still compiles.
- **Bun is dev tooling only.** It builds and tests the TypeScript bridge, and
  installs and runs the template's tools (Vite, TypeScript, the mygo-cli
  package). Nothing Bun-related ships in an app.
- **Great DX over API parity.** The API has the feel of Electron (app
  lifecycle, windows, menus, dialogs) but is Go-first: typed IPC with a
  generated TypeScript client, `http.Handler` for custom protocols, typed
  event structs, blocking calls that are safe from any goroutine.

## Repository layout

```
.                       package mygo: the public API
├── app.go              lifecycle, quit sequence, Dock, paths (paths.go)
├── window.go           Window: native window + its page, events, Eval
├── content.go          Content: windows showing native UI instead of a page
├── ipc.go              Bind/BindAs, method calls, Event[T], CallerWindow
├── plugin.go           Plugin and Use: services bound as "plugin:<name>"
├── channel.go          Channel[T]: values streamed to a call's page
├── typescript.go       GenerateTypeScript / WriteTypeScript (uses internal/tsgen)
├── protocol.go         custom schemes served by http.Handler, FileServer
├── frontend.go         the app's frontend: relative URLs, devUrl, mygo://localhost
├── menu.go             Menu/MenuItem model, roles, native item updates
├── dialog.go modules.go shell, clipboard, screen, theme, tray, shortcuts, notifications
├── loop.go             main-thread queue: postMain / onMain / await
├── events.go           listener lists and the Preventable event types
├── single_instance.go  RequestSingleInstanceLock over a Unix socket
├── dev.go signal_*.go  IsDev, the `mygo dev` ready signal, quitting on SIGINT/SIGTERM
├── backend_*.go        picks the backend per GOOS
├── internal/
│   ├── platform/       the contract every backend implements
│   ├── darwin/         macOS: AppKit + WKWebView through the Objective-C runtime
│   ├── linux/          Linux: GTK 3 + WebKitGTK through dlopen
│   ├── windows/        Windows: Win32 + WebView2 through syscall and COM
│   ├── unsupported/    stub for other platforms
│   ├── fake/           in-memory backend for unit tests
│   ├── bridge/         embeds bridge.js, built from packages/bridge
│   ├── surface/        the connection between a window and its native UI
│   ├── scene/          display lists and glyph atlases, what renderers draw
│   ├── text/           fonts, shaping, line breaking, editing, glyph rasterization
│   ├── vec/            the coverage rasterizer of glyphs and paths
│   ├── raster/         the CPU renderer of scenes
│   ├── gpu/            the instances GPU renderers draw, and gputest/ for their tests
│   ├── gpu/d3d11/      the Direct3D 11 renderer of scenes
│   ├── gpu/metal/      the Metal renderer of scenes
│   ├── tsgen/          TypeScript client generator
│   ├── accelerator/    parses "CmdOrCtrl+Shift+K"
│   ├── update/         update manifests, signatures, archives and delta updates
│   └── e2e/            GUI tests against the real backend (MYGO_E2E=1)
├── packages/           Bun workspace (with the examples' frontends):
│   ├── bridge/         the runtime injected into pages (→ internal/bridge/bridge.js)
│   ├── runtime/        mygo-runtime, the npm package apps and generated clients import
│   └── cli/            mygo-cli, the npm package of the CLI, and in npm/ its
│                       per-platform binary packages
├── plugins/            official plugins, each a Go package and its npm
│                       package (@mygo-plugins/<name>) side by side: fetch,
│                       websocket; and updater, the update window, Go only
├── ui/                 native UI: views, layout, widgets, text editing, Tester
├── cmd/mygo/           the CLI: init, generate, dev, build, doctor
├── examples/           hello, todo, frameless, native, vibrancy; counter-native
│                       and gallery (native UI)
├── docs/               the user guides, the official plugins' pages
│                       (plugins/), and this architecture guide
└── website/            the website, with these docs: TanStack Start, prerendered
                        and served by Cloudflare Workers as static assets
```

## Layers

```
 user code ──► package mygo ──► platform.Backend ──► darwin | linux | unsupported
                 ▲    │               ▲                  │
                 │    └─ handlers ────┘ (AppHandler,     └─ purego ─► AppKit/WebKit, GTK/WebKitGTK
                 │                       WindowHandler)
 page (JS) ◄── bridge.js ◄── Eval (batched) ─┘
     └──── postMessage(JSON) ──► WindowHandler.Message
```

- **`package mygo`** owns all behavior that is not platform specific: option
  defaults, the window registry, event listeners, the quit sequence, IPC
  routing and encoding, menus, protocol handling, trust checks. It never
  calls a native API directly.
- **`internal/platform`** is a small, synchronous contract: a `Backend`
  (event loop, window factory, menus, dialogs, clipboard, …), a `Window`
  (chrome + webview), and two handler interfaces the core implements to
  receive events (`AppHandler`, `WindowHandler`). Options arrive fully
  defaulted; backends never invent policy.
- **Backends** translate the contract to native calls. They keep native
  objects alive, map native callbacks back to Go objects and report events.

Keeping policy in the core is what lets `internal/fake` test almost all
behavior without a GUI, and keeps each backend a thin translation layer.

## Threading model

Cocoa and GTK must be driven from the thread that started the process. The
rules are:

1. **The main goroutine is locked to the main thread** (`runtime.LockOSThread`
   in `mygo.go`'s `init`), and `App.Run` must be called from it. `Run`
   initializes the backend and blocks in the native event loop. Before
   that no backend is initialized (the Linux one has not even loaded GTK),
   yet `main` already runs on the main thread, where `onMain` calls
   functions directly, and generate mode never initializes it. So a public
   method that reaches the backend either keeps its value in the core
   until the backend can take it, for settings (`SetActivationPolicy`
   through `AppOptions`, `SetMenu` once ready, `Theme.SetSource` and
   `Dock.SetMenu` right after `Init`), or starts with `needsApp`, which
   panics on the main thread before `Run` with the name of the call,
   rather than letting it crash on Linux, hang in `await` or answer wrong
   on macOS. Only calls that work before `Init` on every backend (`Locale`,
   packaging info, login items, URL schemes, `IsOnBattery`) do neither.
2. **Every `platform` method is called on the main thread, and every handler
   callback runs there.** The backend never needs locks for its own state.
3. **Every public method is safe from any goroutine.** `loop.go` provides:
   - `postMain(fn)` appends to a FIFO queue and calls `Backend.Signal`, which
     makes the backend call `AppHandler.Dispatch` (→ `loop.drain`) on the main
     thread. macOS uses a `CFRunLoopSource` added in all common modes (so it
     fires while menus track, windows resize and modal panels run); Linux uses
     `g_idle_add`, deduplicated with an atomic flag.
   - `onMain(fn)` runs `fn` directly when already on the main thread, else posts
     it and waits. After shutdown posted work is dropped instead of
     deadlocking.
   - `await(ch)` waits for an asynchronous native result. Off the main thread
     it is a plain channel receive. **On the main thread it pumps native events
     with `Backend.Step`** until the value arrives; whoever produces the value
     calls `deliver`, which sends and then `Backend.Wake`s the loop. This is
     what makes `Page.Eval`, `CapturePage` and dialogs usable from event
     listeners without deadlocking. Nested runs of the dispatch source are
     expected and safe.
4. **Event listeners run on the main thread**, synchronously, so cancelable
   events (`OnClose`, `OnBeforeQuit`, `OnWillNavigate`, …) can answer the
   native toolkit immediately. **IPC calls run on their own goroutines**, so
   bound methods may block.
5. **Validate in the caller's goroutine.** Anything that can panic on bad
   input (for example `WindowOptions.BackgroundColor`) is checked before
   hopping to the main thread, so the panic points at the user's call.

Main-thread-only fields are marked as such in comments (for example
`Window.native`, `Window.trusted`). Fields shared with other goroutines are
guarded by a mutex or atomic.

## Native interop without cgo

purego gives three primitives, used everywhere:

- `purego.SyscallN(fn, args...)` for integer/pointer arguments. Fast, no
  reflection. Floats and structs cannot be passed this way.
- `purego.RegisterFunc(&typedFn, addr)` for signatures with floats or
  structs (`NSRect`, `CGFloat`, `GdkRGBA*` …). These are created once at
  startup; calling them uses reflection, so they are kept off hot paths.
- `purego.NewCallback(fn)` turns a Go function into a C function pointer. At
  most ~2000 callbacks can exist per process and they are never freed, so
  **callbacks are created once per signature at startup** and user data (a
  window id, a request id) identifies the target. Never create callbacks per
  window, per request or per call.

### macOS (`internal/darwin`)

- `objc.go` loads the frameworks, caches selectors and classes, and wraps
  `objc_msgSend`: `send(obj, "selector:", args...)` for integer arguments and
  pre-registered typed variants (`msgRect`, `msgSetFloat`, `msgInitWindow`,
  …) for the rest. On amd64, methods returning structs larger than 16 bytes go
  through `objc_msgSend_stret`.
- Classes are defined at run time with `objc.RegisterClass`: `MyGoWindow`
  (borderless-friendly `NSWindow`), `MyGoWebView` (records the last
  `mouseDown:` for drag regions), `MyGoWindowDelegate` (window, navigation,
  UI and script-message delegate in one object per window), the app delegate,
  scheme handler, menu and tray targets. Only protocols that exist at run
  time are adopted (`WKScriptMessageHandler` is not registered, and WebKit
  does not need it).
- **The web view is not the content view.** A plain `NSView` is, holding the
  web view and, with vibrancy, an `NSVisualEffectView` behind it. WebKit
  docks the inspector next to the web view in its superview; were that the
  window's frame view, AppKit would draw a broken legacy title bar from then
  on.
- **Memory is managed by hand.** Objects created with `alloc`/`init` are owned
  (+1) and must be released; convenience constructors return autoreleased
  objects. Code that creates temporary objects runs inside `withPool`.
  Delegates and windows are released with `autorelease` from
  `windowWillClose:` because AppKit still uses them while closing.
- **Blocks.** Completion handlers passed to Apple APIs are created with
  `objc.NewBlock` and released right after the call (the callee copies
  them). Blocks received from Apple (decision and completion handlers) are
  invoked with `callBlock`; when they are called later they are
  `_Block_copy`d first and released after use.
- **Coordinates.** AppKit's origin is the bottom-left of the primary screen;
  MyGo's is the top-left. `rectToMac`/`rectFromMac` convert.
- **Quitting.** Cmd+Q ends in `applicationShouldTerminate:`, which runs the
  core quit sequence and then stops the run loop so `App.Run` returns (the
  reply is `NSTerminateCancel`; AppKit never calls `exit`). Quit Apple Events
  (Dock > Quit, AppleScript, logout) are handled by our own handler installed
  in `applicationWillFinishLaunching:`, so senders get a success reply, or
  error -128 when a listener cancels.

### Linux (`internal/linux`)

- `ffi.go` `dlopen`s GLib, GObject, GIO, GDK, GTK 3, cairo, GdkPixbuf
  and, when installed, AppIndicator. WebKitGTK 4.1 (4.0), JavaScriptCore
  and libsoup 3 (2.4), which only windows showing web pages need, load
  with the first such window (`webKit`), or to clear browsing data: an app
  that shows only native UI never maps them and the 75 or so libraries
  they bring, some 7 MB and 20 ms of startup. Without them, `errWebKit`
  says what to install when such a window is created. Symbols from newer
  WebKitGTK versions are bound optionally and feature-detected
  (`webkitWebViewCallAsyncJavascriptFunction != nil`).
- Signals are connected with `g_signal_connect_data`, passing the window id as
  user data; `Backend.window(data)` resolves it and ignores closed windows.
  GDK event structs are read at fixed 64-bit offsets (`field[T]`).
- The main thread is the one that called `gtk_init_check`; `IsMainThread`
  compares `gettid`. `Step` is `g_main_context_iteration(NULL, TRUE)` and
  `Wake` is `g_main_context_wakeup`.
- GTK geometry changes are asynchronous: `SetBounds` remembers the requested
  rectangle, within the sizes GTK gives the window (`constrain`), as does a
  window about to show, which X has where GTK created it, or where it was,
  until the window manager places it; `Center` moves that rectangle along.
  `Bounds` reports that rectangle until a configure event reports its size
  and, on X11, its position, or the window manager's own (synthetic)
  report of the size comes, or another size, which the window manager or
  the user chose. A window being placed waits for the window manager's
  report: a reparenting one first puts its frame where it created it. A
  report of the previous size was sent before the window manager took the
  request (openbox sends one when the size hints change); GTK would ask
  for that size again from it, so the backend asks for the new one again.
- A window the user cannot resize is never smaller than its default size,
  which `SetBounds` sets too, and `SetResizable(false)` to the size it has,
  or than its natural size, which GTK makes 200x200 when the window's child
  has none, as the web view: the box around the web view asks for 1x1.
- GTK gives windows without decorations no resize borders, so the outer
  5 px of the page of a frameless window, or one with a hidden title bar,
  resize it (16 px along the edges from a corner resize the corner). The web view's `motion-notify-event` shows
  a resize cursor there, keeping WebKit's cursor to restore, and its
  `button-press-event` calls `gtk_window_begin_resize_drag` with the press,
  as a Wayland compositor requires; neither event reaches WebKit. Nothing
  resizes a maximized or full screen window, and a tiled one only resizes
  at the edges the window manager allows, as with GTK's own decorations.
- A hidden title bar (`titlebar.go`) is a window without decorations whose
  web view is in a `GtkOverlay`, under a `GtkHeaderBar` for each side of
  `gtk-decoration-layout` that names window buttons. Each bar shows only
  that side's minimize, maximize and close (`decoration-layout`), and CSS
  clears its background and, with `TitleBarHeight`, its minimum height.
  GTK makes the buttons (they need the bar inside a `GtkWindow`) and hides
  those the window cannot use, and they act as a header bar's: iconify,
  toggle maximized, close. The bars are measured before the web view
  exists, from their natural size, for the script that tells the first
  page, then from their allocations; a change of the layout setting
  rebuilds them. A Wayland compositor that decorates windows itself
  (`gdk_wayland_display_prefers_ssd`: the default mode of
  `org_kde_kwin_server_decoration_manager`, server on KWin, Hyprland and
  Sway) gets no bars: GTK asks it to decorate a window without decorations
  too, so its title bar, or a tiling compositor's lack of one, stands for
  the buttons, whatever the layout says.
- `WEBKIT_DISABLE_DMABUF_RENDERER=1` is set unless the user set it, which
  avoids blank webviews on NVIDIA drivers, VMs and containers.
- XDG desktop portal calls go through `portalCall` (`portal.go`), which
  first registers the app's ID, the name of its desktop entry, with
  `org.freedesktop.host.portal.Registry`: the portal only accepts that
  before any other call, and needs it for apps outside a sandbox that the
  desktop did not launch. Methods that may involve the user answer with a
  `Response` signal on a request object; `portalRequest` routes it to a
  callback by the object's path.
- On Wayland, where apps grab no keys, global shortcuts go through the
  `GlobalShortcuts` portal (`hotkey_portal.go`). A session binds its
  shortcuts once, so every change closes the session and creates one that
  binds them all; the ids are canonical accelerators and the session token
  is named after the app, so desktops keep the keys the user picked. The
  portal may ask the user to confirm new shortcuts and needs the app's ID
  (portals from 1.21 refuse without it), so the app must be installed.
  KDE Plasma 5 binds the shortcuts listed in `CreateSession`'s options, as
  the portal's drafts had it: portals before 1.17 pass them on, later ones
  drop them and Plasma 5 then binds nothing; MyGo logs the shortcuts the
  desktop bound no keys to. The activation token of an `Activated` signal
  becomes the display's startup notification id while the callback runs,
  so the window it shows or focuses may take the focus.

### Windows (`internal/windows`)

- Win32 is called through `syscall` (`LazyDLL` procs; system DLLs are loaded
  by absolute path from the system directory). `IsMainThread` compares the
  thread id recorded during package initialization. The loop is
  `GetMessageW`; `Signal` and `Wake` post messages to a hidden top-level
  application window, which also receives tray, hot key, theme
  (`WM_SETTINGCHANGE`) and display broadcasts. `Quit` posts `WM_QUIT`, which
  modal loops (dialogs, menus) forward, so quitting works while they run.
- **COM.** `comCall` calls vtable methods; its indices come from
  `WebView2.h` (constants in `webview2.go`). Handler objects (event and
  completion handlers) share one vtable whose callbacks are created once and
  route `Invoke` to a Go function per object; the objects live in a map until
  COM releases them. Call wrappers are `go:uintptrescapes`, so
  `uintptr(unsafe.Pointer(&x))` arguments stay valid.
- **WebView2 without a loader DLL.** `createEnvironment` finds the Evergreen
  runtime in the registry (`EdgeUpdate\ClientState\<channel>\EBWebView`)
  and calls `CreateWebViewEnvironmentWithOptionsInternal` of its
  `EmbeddedBrowserWebView.dll`, as `WebView2Loader.dll` does; a loader next to
  the executable wins. The first window that shows a web page starts the
  environment (`startEnvironment`), and fails without a runtime: windows
  that show native UI need none, so an app whose windows all do runs
  without WebView2. The environment and each controller are created
  asynchronously: window methods that need the webview wait in `pending`.
  User data lives in `%LOCALAPPDATA%\<name>\WebView2`.
- **Custom schemes** load from `http://<scheme>.localhost/`, which WebView2
  lets the app answer through `WebResourceRequested`. Chromium treats
  `*.localhost` as a secure origin, and unlike `https`, `http` blocks no
  mixed content, so pages reach `ws://` and `http://` URLs as with the
  custom schemes of the other backends. The backend maps URLs both ways, so
  the core only sees `<scheme>://localhost/`.
  Responses are buffered: WebView2 takes a whole stream. `LoadHTML` with a
  base URL serves the document from that URL the same way.
- **Eval** goes through the DevTools protocol (`Runtime.evaluate` with
  `awaitPromise`), which reports syntax errors and ignores the page's CSP.
  Edit roles run `document.execCommand` with a user gesture; paste inserts
  the clipboard text.
- Scripts injected at document creation run in every frame, so the
  main-frame-only ones (the bridge) are wrapped in `window === window.top`.
  Iframes get no `chrome.webview` handler.
- **ABI.** On x64, structs over 8 bytes are passed by reference and Go
  mirrors the first integer arguments into the XMM registers, so doubles can
  be passed as bits. On ARM64, 16-byte structs travel in two registers and
  floats cannot be passed, so zoom falls back to CSS (`abi_*.go`). Go
  callbacks read only integer registers: the two methods of UI Automation
  that take doubles enter through thunks in assembly (`uia_*.s`), which
  move the doubles' bits to the integer registers of their positions,
  touching only registers a call may change, and jump to the callbacks.
- DPI: the process is per-monitor aware (v2); the backend converts between
  pixels and DIPs with the window's or monitor's DPI. Frameless windows, and
  those with a hidden title bar, drop the caption in `WM_NCCALCSIZE` but
  keep the side and bottom borders, which Windows 10+ draws invisibly
  outside the window, so they still resize. On Windows 11 a hidden title
  bar keeps one pixel of its top, under the window's border, without which
  the maximize button opens no snap layouts while the window is not
  maximized. Windows 10, which has no snap layouts, keeps none: over a
  window that keeps any of its top it draws its whole title bar. The build
  comes from `RtlGetVersion`, which no manifest changes.
- A hidden title bar (`titlebar.go`) gets its controls from two child
  windows above the webview's. The buttons are a layered window
  (`UpdateLayeredWindow`, premultiplied BGRA): Segoe Fluent Icons glyphs
  (Segoe MDL2 Assets on Windows 10) drawn white on black with GDI give
  their coverage, over backplates of Chromium's alphas. It answers
  `WM_NCHITTEST` with `HTMINBUTTON`, `HTMAXBUTTON` and `HTCLOSE`, passes
  `WM_NCMOUSEMOVE` to `DefWindowProc`, which opens Windows 11's snap
  layouts over maximize, and runs the buttons itself from the non-client
  button messages, sending the window `WM_SYSCOMMAND`. The top edge is a
  layered window without a bitmap (`WS_EX_NOREDIRECTIONBITMAP`), as tall as
  the sizing frame, that answers `HTTOP` and sends the window's presses on,
  so it resizes; Windows Terminal's caption works the same way. The
  webview's window is created later, so the controls go back to the top of
  the z-order when it appears. A window without a caption has no room for
  its menu bar: Alt and F10 open a popup holding the bar's own submenus.
- Message boxes are task dialogs (comctl32 v6, activated from shell32's
  manifest for executables without one); their structs are packed and laid
  out by hand. Notifications are notification-area balloons, which Windows
  10+ shows as toasts.

## IPC

### The page runtime (`packages/bridge` → `internal/bridge/bridge.js`)

`packages/bridge/src/bridge.ts` is bundled by Bun into an IIFE
(`bun run build`) and committed, so building an app never needs Bun. The core wraps it with the
window's configuration (`bridge.Script`) and every backend injects it at
document start into the main frame. It installs:

- `window.mygo`: `call(method, ...args)`, `on(event, fn)`, `once`, the
  `window` controls (`minimize`, `toggleMaximize`, `close`, …), `platform`,
  `windowId`, `version`. Frozen, so pages cannot tamper with it.
- `window.__mygo.receive(messages)`, used by Go to deliver messages.
- dom-ready notification and `--app-region: drag` handling for frameless
  windows (the mousedown is reported, the backend starts a native window drag
  from the recorded mouse event).
- The `--mygo-titlebar-*` CSS variables of a window with a hidden title
  bar, in a constructed style sheet (a `<style>` element where engines
  lack them), from the `mygo:title-bar` event: the room its controls take
  (`Window.TitleBar` of the backend, in CSS pixels). Each backend delivers
  the event at document start, in a script after the bridge, so the first
  page lays out around the controls before it paints; the core sends it
  again when the room changes and once the DOM of any later page is ready.
- File drops (`Window.OnFileDrop`, `onFileDrop` in mygo-runtime). Unlike
  Tauri, whose native drop handler takes drops away from the page (on
  Windows, HTML5 drag and drop needs it turned off), MyGo leaves the page's
  drag and drop alone and reads the paths next to it. A capture listener on
  `drop` posts `{t:"drop", x, y}` when the drop carries files; the core then
  takes the paths the backend recorded just before the page saw the drop:
  `performDragOperation:` of the web view subclass (calling super) on
  macOS, `drag-data-received` then `drag-drop` (before WebKit's own
  handlers) on Linux, and on Windows the File objects the bridge posts with
  `postMessageWithAdditionalObjects` (`ICoreWebView2File.Path`), with
  WebView2's own drop handling left on. Drags that start in the page are
  ignored (`dragstart`/`dragend` tracking), and so are drops the page
  handles; where the page does not handle dragged files, bubbling
  `dragover`/`drop` listeners cancel them, so a drop reaches Go instead of
  the engine replacing the page with the file. Paths go to Go listeners and,
  as the `mygo:file-drop` event, to trusted pages only. Event names starting
  with `mygo:` are reserved.

- Find in page (`find.ts`, `Page.FindInPage`): the bridge walks the
  visible text nodes, marks matches with the CSS Custom Highlight API (a
  constructed style sheet, which a Content Security Policy allows) and
  scrolls to the active one; engines without the API get the active match
  selected instead. Being JavaScript, it works the same in every engine.

The transport is `window.webkit.messageHandlers.mygo.postMessage` on both
WebKit platforms and `window.chrome.webview.postMessage` on WebView2.

### The `mygo-runtime` package (`packages/runtime`)

Apps reach the injected runtime through the `mygo-runtime` npm package:
`call`, `on`/`once`, typed `event<T>(name)`, `currentWindow` controls,
`isMyGo`, `isCallError`, `runtime()` (which throws a helpful error outside a
MyGo window) and the public types (`Runtime`, `WindowControls`, `Platform`),
which the bridge shares. It holds no transport of its own: it delegates to
`window.mygo`, so the injected script stays the single implementation of the
protocol. Its `dist/` is not committed: `bun run build` builds it, as CI
and releases do, and must have run in a checkout that `mygo init --mygo
<checkout>` depends on with `file:`. The template depends on `^<version>`
from npm, released in step with the Go module.

### The `mygo-cli` package (`packages/cli`)

The CLI is also published to npm, so that projects pin it in package.json
and run it from their scripts. Like esbuild, each platform's binary is a
package of its own, `@egoist/mygo-cli-<os>-<cpu>` in `packages/cli/npm` (npm
takes unscoped names such as `mygo-cli-win32-x64` for spam), with `os`
and `cpu` fields; `mygo-cli` lists them all as optional dependencies, so
package managers install only the matching one, and its `bin/mygo.js`
resolves that package and replaces itself with the binary
(`process.execve` in Node.js 23.11 and later and in Bun; elsewhere it spawns
the binary, waits and passes its exit status on). `MYGO_CLI_BINARY` points
it at another build. In a checkout of this repository, where the platform
packages hold no binary, it builds `cmd/mygo` from source instead: the
workspace examples run it that way.

`bun run --cwd packages/cli binaries [platform...]` cross-compiles the
binaries (ignored by git) and writes the manifests with the version of
`mygo.Version`; `bun scripts/publish.ts` publishes the packages (see
[Releasing](#releasing)). The binary is named `mygo`, like an unrelated npm
package: docs say `bunx mygo-cli`, never `bunx mygo`, outside a project.

### Wire protocol

Page → Go (a JSON string per message, prefixed with the window's secret; see
Trust):

| message | meaning |
|---|---|
| `{"t":"call","id":N,"k":token,"m":"Service.Method","a":[...]}` | call a bound method |
| `{"t":"chan-ack","c":N,"k":token,"n":S}` | the page took the values of channel N up to the S-th |
| `{"t":"chan-close","c":N,"k":token}` | the page closed channel N |
| `{"t":"dom-ready"}` | DOMContentLoaded fired |
| `{"t":"drag"}` / `{"t":"dblclick"}` | mousedown / double click on a drag region |

Go → page, batched into one `__mygo.receive([...])` evaluation per
main-loop turn. JavaScriptCore runs a script of that shape, `a.b(JSON)`,
with its JSON parser instead of compiling it, which is several times
faster, unless the inspector is enabled (development builds):

| message | meaning |
|---|---|
| `{"t":"reply","id":N,"k":token,"ok":true,"v":value}` | successful call |
| `{"t":"reply","id":N,"k":token,"ok":false,"e":"message"}` | error or panic |
| `{"t":"chan","c":N,"k":token,"p":value}` | a value of channel N; `"a":1` asks for an acknowledgment |
| `{"t":"chan","c":N,"k":token,"end":true}` | Go closed channel N |
| `{"t":"event","n":"name","p":payload}` | typed event |

`k` is a random per-page token: a reply meant for a page that has since
navigated away can never resolve a promise of the new page.

### Calls

1. `WindowHandler.Message` runs on the main thread. Messages starting with
   `{"t":"call",` are handed to a goroutine (`handleCall`) together with the
   page context and the page's trust decision, captured on the main thread so
   a racing navigation cannot change them. Everything else is decoded on the
   main thread.
2. `handleCall` decodes the envelope with `encoding/json/v2`, looks up the
   method, decodes each argument into the parameter type, calls it and
   encodes the result. Panics are recovered, logged with a stack trace and
   returned as errors. Large values are copied as little as possible: the
   arguments are raw values that share the message (`rawValue`), decoded
   from it in place, and the result is encoded into a part of the reply
   (`message`), which the flush copies into the script once.
3. The reply is queued with `Window.enqueue(msg, false)` and flushed on the
   main thread.

Bound methods may take a `context.Context` first: it carries the calling
window (`CallerWindow`) and is canceled when the page navigates or the window
closes. `Bind` validates every parameter and result type up front
(`tsgen.Validate`), so unsupported types fail at startup, not at call time.

JSON options (`jsonOptions` in `ipc.go`) are shared by calls and events:
json/v2 defaults (nil slices encode as `[]`, case-sensitive names),
`time.Duration` as nanoseconds, and U+2028/2029 escaped for JavaScript.

### Events

`NewEvent[T](name)` registers a typed event. `Emit` and `Broadcast` encode
once and enqueue per window. **Events are held until the page reports
dom-ready** (bounded to 1024 per window) and the hold resets on every
navigation, so events sent right after creating a window, or during a
navigation, are delivered once listeners exist. Replies are never held:
module scripts may `await` a call at top level, and DOMContentLoaded waits for
them.

### Channels

A `*Channel[T]` parameter (`channel.go`) streams values to the page that
made the call. The page's `Channel` goes into the call's arguments as an
id it chose; `method.call` creates the Go side for that page, registered
in `Window.channels`, and closes it when the method returns, before the
reply is queued, so the page gets the values, the end, then the result.
The page may close it earlier, which cancels the call's context, a context
derived from the page's for calls with channels; so does the page going
away. The page may even close it before the call's goroutine made it
(aborting a request right after starting it): the window remembers such
closes (`closedEarly`, bounded, reset with the page) and the call closes
the channel as soon as it makes it. Values are queued in the window's outbox like replies, so they are
batched and stay in order with them.

Flow control keeps a producer faster than the page from piling up
messages: every half MiB of messages, a value asks for an acknowledgment,
which the page sends once it took that value (handled it, or yielded it to
an iterator), and `Send` waits while more than a MiB is unacknowledged. It
never waits on the main thread, which receives the acknowledgments.

### Eval

`Page.Eval(code)` uses the webview's native async-function API
(`callAsyncJavaScript` on macOS, `webkit_web_view_call_async_javascript_function`
on Linux), which awaits promises and ignores the page's Content Security
Policy. The code is first wrapped as `return JSON.stringify({ok, v: await (code)})`;
if that fails to compile the code is not an expression, and it is run again as
a function body. A compile error means nothing executed, so code never runs
twice. The result travels as a JSON string and is decoded in Go.

### Trust

Every page gets the runtime, but only trusted pages may call Go: the
frontend (`mygo:` and the `mygo dev` server), custom-scheme pages
(`Protocol.Handle`), `file:` and `about:` pages, loopback `http(s)` dev
servers in development, and origins listed in `PageOptions.TrustedOrigins`
(`"*"` trusts everything). The decision is
recomputed on every committed navigation. Untrusted calls are rejected
without running any Go code.

The message handler is reachable from every frame, while trust is decided
by the main frame's URL, so an iframe of another origin must not be able to
talk to Go: each window has a random secret (`crypto/rand`), passed only to
the bridge's closure in its configuration, which prefixes every message;
`handleMessage` drops messages without it. macOS additionally only accepts
messages from the main frame (`WKScriptMessage.frameInfo.isMainFrame`);
WebKitGTK exposes no frame information, so Linux relies on the secret.

### Plugins

`Use` binds a plugin's service as an internal service named
`plugin:<name>`: calls reach it like any bound method (trust checks,
contexts, channels), but `GenerateTypeScript` skips it, since the plugin's
own npm package is its client. A call to a plugin that is not used fails
with an error naming `mygo.Use`. The official plugins in `plugins/` keep
each Go package next to its npm package, built into `dist/` by `bun run
build` like mygo-runtime and released with the same version.

- **fetch** streams a response through a `Channel`: the head first (status,
  headers, final URL), then base64 chunks of the body as Go reads them. The
  JavaScript side builds a `Response` around a pull-based `ReadableStream`
  over the channel's iterator, so the page's reading paces Go through the
  channel's flow control. Aborting, canceling the body or the page going
  away closes the channel, which cancels the request's context. Request
  headers go through a plain `Headers`, which, unlike a `Request`'s, drops
  no forbidden names.
- **websocket** is an RFC 6455 client of its own (no dependency) on top of
  `net/http`, which keeps upgrade requests on HTTP/1.1 and hands the
  connection over as the body of the 101 response, so proxies and the
  client's TLS settings apply. `Connect` streams the connection's events
  through a channel for as long as it lasts; the page sends with `Send`
  calls numbered in order, since calls run on goroutines of their own and
  would otherwise race, and Go writes them in that order. Connections are
  keyed by window and a random id the page chooses.
- **updater** is the update window, in the manner of Sparkle, built on
  `mygo.Updater` alone. A *session* is a check and what follows it (the
  release notes, the download, the offer to relaunch): a goroutine that
  sets the session's *view* (title, message, progress, rendered notes,
  buttons) and waits for responses, whether or not the window shows, so
  that a background check shows it only when it has something to offer and
  a "Check for Updates…" during one just shows it. The window's page, one
  embedded HTML file loaded with `LoadHTML` (an `about:blank` page, so
  trusted), watches the views through a `Channel` (`Watch`) and answers
  with `Respond`; each set of buttons has a prompt number and only the
  first answer to the current prompt counts, so a double click cannot
  answer the next view. The plugin's service rejects calls from other
  windows. Release notes are Markdown rendered in Go (`markdown.go`), which
  escapes all HTML and only links http(s) and mailto URLs, and a
  Content Security Policy with a nonce runs only the page's own script:
  the page may call Go, and the notes come from the unsigned manifest.
  Links open in the browser (`OnWillNavigate`). Views with release notes
  have a fixed size; status views ask for the height of their text
  (`Fit`). The window gets an empty menu of its own, so it has no menu bar
  on Linux and Windows. `updater.json` in `PathUserData` keeps the
  preferences, the skipped version and the time of the last check, and
  a change that alters them calls the functions of `OnChange` on the main
  thread, from a goroutine of its own so that no caller waits for the main
  thread; the next check is due an interval after it, or an hour after a failure, and
  is rescheduled on resume since timers stop while the computer sleeps. An
  update installed while the app runs is remembered, so that checks offer
  to relaunch instead of installing it again. The texts are `Strings` in
  the language that best matches `Options.Language` or `App.Locale`
  (`matchLanguage`: language, script, region; Chinese scripts inferred
  from regions such as TW; another variant of the language before
  English), among the plugin's translations (`translations.go`) and the
  app's, whose empty fields fall back to the plugin's, then English. The
  page gets `lang`, which picks CJK fonts, and `dir`; status texts use
  `unicode-bidi: plaintext` and the notes `dir="auto"`, as either may be
  in another language than the window. The page reports the width its
  buttons need too, as translations can be long.

## Typed client generation (`internal/tsgen`)

`mygo generate` builds the app and runs it with `MYGO_GENERATE=<file>`;
`App.Run` then writes the client (`WriteTypeScript`) and returns before
touching the GUI. The generator combines:

- **Reflection** for correctness: types follow json/v2 encoding (embedded
  structs are inlined with v2's conflict rules, `omitempty`/`omitzero` become
  optional fields, pointers become `T | null`, `[]byte` is a base64 string,
  `time.Time` a string, types with JSON methods `unknown`, text marshalers
  `string`, generic instantiations get names like `PageTask`).
- **Source code** for readability, when available: the entry PC of each bound
  method (`runtime.FuncForPC`) locates its file, which is parsed with
  `go/parser` to recover parameter names and doc comments (JSDoc). Named
  string/number types with constants become union types (`"all" | "done"`),
  including simple `iota` sequences. Packages not reached through a PC are
  located with `go list`. Without sources (e.g. `-trimpath` binaries) the
  client is still correct, just with `arg0` names and no docs.

The output imports `call` and `event` from `mygo-runtime` and declares the
interfaces, one object per service with camelCased methods, and an `events`
object. `WriteTypeScript` only rewrites the file when its
content changes, so dev servers don't reload needlessly.

## Custom protocols

`Protocol.Handle(scheme, http.Handler)` lets pages load `<scheme>://localhost/…`
like a web origin (fetch, ES modules, relative URLs). `Protocol.serve` runs the
handler on a goroutine with a `schemeWriter`: headers and body are buffered
and handed to the main thread in 256 KiB chunks (and on `Flush`), the content
type is sniffed when missing, panics become 500 responses. Backends that
implement `platform.SchemeBodyWriter` take the body on the handler's
goroutine instead, where they may block it, and the chunk buffer is reused.
Backends turn responses into native ones:

- macOS: `WKURLSchemeHandler`; `didReceiveResponse:`/`didReceiveData:`/
  `didFinish`. A task stopped by WebKit cancels the request context and later
  writes are ignored (touching a stopped task raises an Objective-C exception).
- Linux: WebKitGTK wants a `GInputStream`, so the response body is streamed
  through a pipe (`g_unix_input_stream_new`), which WebKit reads on the main
  loop, 8 KiB at a time. The handler's goroutine writes the body into the
  pipe (`WriteBody`), waiting while it is full, so the main loop never
  blocks and a response WebKit reads slowly does not pile up in memory.
  Custom schemes are registered as secure and CORS-enabled.

Schemes are registered per webview at creation time, so call
`Protocol.Handle` before creating windows. `FileServer(fsys)` serves an
`fs.FS` with an index.html fallback for client-side routing.

## The frontend (`frontend.go`)

Apps load their web UI with URLs without a scheme (`WindowOptions.URL: "/"`,
`LoadURL("/settings")`), which `resolveURL` resolves against the frontend,
as in Tauri:

- during `mygo dev`, the dev server: `MYGO_DEV_URL`, from `devUrl` of the
  configuration (only honored when `IsDev`);
- otherwise `mygo://localhost/`. The `mygo` scheme is registered with every
  webview and, unless the app handles it with `Protocol.Handle`, serves the
  files given to `SetFrontend`. `mygo build` calls `SetFrontend` from a
  generated file that embeds `frontendDist` (see CLI), so app code has no
  `//go:embed` and development builds need no built frontend. During
  `mygo dev` without a dev server it serves `MYGO_FRONTEND_DIST` from disk,
  and with nothing to serve, a page explaining how to get a frontend.

## Windows, lifecycle and quitting

- `NewWindow` validates options, waits for readiness when called off the main
  thread, builds `platform.WindowOptions` (all defaults applied, bridge and
  preload scripts, registered schemes) and registers the window.
- A window's page API is on `Page`, a handle on the window that
  `Window.Page` returns unless the window shows native UI; the page's state
  stays in `Window`, where IPC and the backend's callbacks reach it.
- The user's close (`WindowHandler.ShouldClose`) and `Window.Close` both emit
  `OnClose`, which can be prevented. `Destroy` skips it. The backend reports
  `Closed` synchronously; the core unregisters the window, closes child
  windows, cancels the page context and, when it was the last window and no
  quit is in progress, runs `OnWindowAllClosed` listeners or quits.
- The quit sequence (`Application.prepareQuit`) is: `OnBeforeQuit` → close
  every window (any `OnClose` can cancel) → `OnWillQuit` → stop the loop →
  `App.Run` returns → `OnQuit`. It is used for `App.Quit`, Cmd+Q, quit
  Apple Events, and SIGINT/SIGTERM alike (`signal_unix.go`; a second signal
  exits immediately).
- Taskbar state on Windows (`taskbar.go`) lives on the window and is
  applied when Explorer sends `TaskbarButtonCreated` (first show, Explorer
  restarts), so progress and a hidden button set before showing stick.
- `WindowOptions.StateKey` (`window_state.go`) remembers a window's normal
  bounds and maximized/full screen state in `window-state.json` in
  `PathUserData`, like Tauri's window-state plugin. The state is captured
  300 ms after the last move/resize/state event (transitions resize the
  window on the way, and the normal bounds only change in the normal
  state) and when the window closes; it is written when a window closes
  and when the app quits. A saved window that would not show on any
  display keeps its size and is centered. `WindowOptions.Maximized` starts
  a window maximized: `zoom:` on macOS, `gtk_window_maximize` before
  mapping on Linux, `SW_SHOWMAXIMIZED` on the first show on Windows, where
  `Maximize` on a hidden window also waits for it to be shown.
- Deep links (`deeplink.go`). `mygo build` and `mygo dev` link the name,
  version, identifier and `urlSchemes` of the configuration into the binary
  (`-X …packageName=…`), which is how Linux builds know them (`IsPackaged`,
  `Name`, `Version`) and every platform knows which launch arguments are
  deep links. URLs of those schemes, and of ones registered with
  `RegisterURLScheme`, reach `OnOpenURL`: from Apple Events on macOS, from
  the launch arguments once the app is ready, and from the arguments a
  second instance forwards. `RegisterURLScheme` writes
  `HKCU\Software\Classes\<scheme>` on Windows and, on Linux, a hidden
  `<id>.url-handler.desktop` in `$XDG_DATA_HOME/applications` made the
  default in `$XDG_CONFIG_HOME/mimeapps.list` (what `xdg-mime default`
  does); on macOS it calls `LSSetDefaultHandlerForURLScheme` for a scheme
  the Info.plist declares. The Linux `.desktop` of `mygo build` also
  declares the schemes (`Exec=… %u`, `MimeType=x-scheme-handler/…`).
- Starting at login (`login.go`): `SetOpenAtLogin` registers the bundle
  with `SMAppService.mainAppService` on macOS 13+ (a launch agent running
  `open -a` on macOS 12; both need a bundle), writes an XDG autostart entry
  on Linux, and a value of `HKCU\…\CurrentVersion\Run` on Windows, where
  `OpenAtLogin` also honors Task Manager's `StartupApproved` (odd first byte:
  disabled). The Linux, Windows and launch agent commands pass
  `--mygo-opened-at-login`, which the package removes from `os.Args` at
  init and `WasOpenedAtLogin` reports; macOS login items are recognized by
  `keyAELaunchedAsLogInItem` in the launch Apple Event instead.
- `Relaunch` runs the quit sequence, then (after `OnQuit`, which releases
  the single instance lock) starts the executable again with the same
  arguments and working directory. A development build exits with code 75
  instead, and `mygo dev` starts the same build again, also when that
  happens before it was ready.
- Updates (`updater.go`, `internal/update`, `cmd/mygo/updates.go`), in pure
  Go on every platform. `mygo keygen` creates an Ed25519 key pair; with
  `updates` in the configuration (the public key, and a GitHub repository or a base
  URL) `mygo build` links the manifest URL of the target and the public key
  into the app, and when the private key is available
  (`MYGO_UPDATER_PRIVATE_KEY` or `updates.privateKey`) archives the app as
  installed (the bundle, else everything next to the executable) into
  `<name>-<version>-<target>.tar.gz` (Linux builds always get it, for
  `install.sh`), signs its SHA-256, and writes
  `update-<target>.json` with the `## <version>` section of CHANGELOG.md as
  notes. `Updater.Check` fetches the manifest (HTTPS only, loopback HTTP for
  tests) and compares versions semantically; `Update.Install` streams the
  archive next to the app, verifies size and signature, unpacks it (files,
  directories and relative links only) and swaps it in: the bundle is
  renamed on macOS, the entries of the app directory on Linux and Windows,
  where the running executable is renamed away and removed at the next
  launch. `App.Relaunch` then starts the new version from the path the app
  started from. Development builds are never updated. Delta updates work
  as Sparkle's (`internal/update/delta.go`): `mygo build` fetches the
  published manifest of the target, downloads the archives of up to
  `updates.deltas` versions (that manifest's, and those it lists as
  `previous`, which the new manifest passes on), checks their signatures
  and writes a signed delta from each. Its index lists the tree of the new
  app, inside the bundle on macOS, with each file's size and SHA-256 and
  how to make it: a copy of a file of the old app with the same content
  (at its path, or elsewhere for moved files), else a patch of the file at
  its path or its own bytes, whichever is smaller. Patches are bsdiff's
  (`bsdiff.go`, with the qsufsort suffix array of `suffix.go`), their
  three streams compressed apart so that they are read from the delta
  file in place. `Update.Install` takes the delta whose `from` is the
  running version, makes the new app next to the old one through
  `os.Root`s, and fails on any file that does not match, then downloads
  the archive; the delta also names the versions it updates and makes.
  The tree made is the signed one byte for byte, so bundles keep their
  code signature. `mygo build -upload` publishes to `updates.github` with
  the `gh` CLI: it creates the release `<tagPrefix><version>` as a draft
  with the changelog section as notes, uploads installers, update
  archives and deltas, then the manifests, and leaves
  publishing the draft (which makes the manifests "latest") to the
  developer once every platform is there. With a `tagPrefix` other than
  `v` the repository may hold other releases, so apps are built with a
  feed naming the manifest in the release `<tagPrefix>{version}`
  (`update.TaggedFeed`), which `update.ResolveFeed` turns into the manifest
  of the newest published, non-prerelease release with that prefix, from
  the GitHub API's list of releases, newest first. `Updater.Check` and the
  delta step resolve it in Go, and `install.sh` in sh (`latest_tag`, which
  reads the `tag_name`, `draft` and `prerelease` fields in order). Such
  drafts are made with `--latest=false`, and published the same way, so
  the repository's latest release stays the others'. With `updates.s3` it puts the
  same files, manifests last, into a bucket of S3 or a compatible service
  (`cmd/mygo/s3.go`): one `PUT` per object, signed with AWS Signature
  Version 4 in pure Go (`crypto/hmac`), its SHA-256 payload hash checked
  by the service; the bucket goes in the host name on AWS, and in the path
  with a custom `endpoint` or a bucket name with dots, unless `pathStyle`
  says otherwise.
- File associations (`fileAssociations` in the configuration) are declared by the
  packages (see the table below) and their extensions linked into the
  binary; like deep links, files of those extensions among the launch
  arguments and those a second instance forwards reach `OnOpenFile`
  (paths relative to the working directory, and file URLs from `%U`).
- Downloads (`downloads.go`): links with `download`, and responses a page
  cannot show or that are attachments, become downloads. `OnWillDownload`
  gets the URL, the suggested name and a default path in Downloads (a
  unique name), which it may change or cancel; `OnDownloadDone` reports
  the result. WebKit cannot turn what a custom scheme handler serves into a
  download, and WebView2 would fetch it over the network, so those are
  canceled and the core serves the URL again through the scheme's handler
  into the file (`SchemeDownload`).
- Permissions (`permissions.go`): the backends route camera, microphone,
  location and notification requests to `WindowHandler.PermissionRequested`;
  `SetPermissionHandler` decides them, and by default trusted pages (the
  app's own, `TrustedOrigins`) are granted and others denied. Custom scheme
  pages are secure contexts with a real origin on every platform, which
  these APIs need. `macos.infoPlist` adds keys such as the camera and
  microphone usage descriptions macOS requires.
- `window.open()` and `target=_blank` go through `SetWindowOpenHandler`. By
  default http(s) URLs open in the default browser. Allowing one creates a
  window around the configuration or related view WebKit provides, with its
  own content manager so scripts and messages never leak between windows.

## Menus

`Menu`/`MenuItem` are plain Go values built from templates. Roles expand into
labels, accelerators and submenus per platform (`roleDefaults`,
`roleSubmenu`); macOS-only roles are hidden elsewhere. Items get a process
unique id and are registered with weak pointers, so discarded context menus
can be collected. The core sends immutable snapshots (`platform.Menu`) to the
backend, which:

- builds native menus and tracks native items per owner (app menu, window menu
  bar, tray, popup) so `UpdateMenuItem` can change label/state in place and
  rebuilt menus release their items,
- performs edit roles natively (first responder on macOS,
  `webkit_web_view_execute_editing_command` on Linux), or sends them to a
  window showing native UI as a `SurfaceCommand`, and reports everything
  else through `AppHandler.MenuItemClicked`; the core toggles checkbox/radio
  state, performs window and view roles and calls `Click`.

macOS gets a default menu bar (App, File, Edit, View, Window), which is what
makes Cmd+C/V/Q work; other platforms get none unless the app sets one.

## Native UI (`ui`)

A window with `WindowOptions.Content` shows a user interface MyGo draws
itself instead of a web page. The layers stay as everywhere else: the
toolkit (`ui`) is plain Go above the platform contract, a backend only
provides a surface to draw on and its input, and renderers know nothing of
either.

```
 view (Go) ──► ui: build, layout, paint ──► internal/scene ──► internal/gpu/d3d11 | internal/raster
                 ▲       └─ internal/text: DirectWrite | Core Text | Pango, glyph and mask atlases
                 │
 internal/surface.Conn ◄── content.go (package mygo) ◄── platform.Surface: darwin | linux | windows | fake
```

- **The surface.** With `platform.WindowOptions.Surface`, a backend creates
  a view MyGo draws in place of the webview: a layer-backed NSView on
  macOS, a GtkGLArea on Linux (a GtkDrawingArea where OpenGL would not run
  on a GPU), a child window of class `MyGoSurface` on Windows.
  `platform.Surface` gives its native handles (for a swap chain, a layer,
  or the GtkGLArea while its `render` signal draws a frame, with its
  context current), size and scale; asks for a frame (`RequestFrame`: a
  paused `CADisplayLink`, before macOS 14 an `NSTimer` at the display's
  rate, `gtk_widget_queue_draw`, `InvalidateRect`); presents
  pixels drawn on the CPU (`PresentPixels`: a CGImage as the layer's
  contents, cairo in the `draw` signal, `SetDIBitsToDevice` in `WM_PAINT`);
  sets the cursor; and turns the input method on and off at the caret
  (NSTextInputClient, GtkIMContext, IMM32), with the text around it, up to
  512 runes on each side (`SetTextInput`). Everything else comes through
  `WindowHandler.SurfaceEvent`, in DIPs: frames, resizes, the pointer, the
  wheel, keys, text, compositions, focus, the edit roles of menus
  (`SurfaceCommand`), files dragged and dropped, and assistive technology.
  Input methods name what text and compositions replace in the text
  around the caret (`Replace`, `From`, `To`): NSTextInputClient's
  replacement ranges, GtkIMContext's `delete-surrounding`, IMM32's
  reconversion (`IMR_CONFIRMRECONVERTSTRING`). Files dragged over the
  surface (NSDraggingDestination, a GTK drag destination, an OLE
  `IDropTarget`) are `FileDragOver` events, whose answer the drag source
  shows, and a drop a `FileDrop`; files the content does not take go to
  `OnFileDrop`.
- **Assistive technology.** Once it asks for a surface's content, the
  backend sends `AccessibilityOn`, and the engine describes every frame
  (`ui/access.go`) as a `platform.AccessTree`: nodes in pre-order with
  roles, names, values, ranges, states, bounds and actions, under
  `UpdateAccessibility`. Backends keep a native object per node ID and
  tell the system what changed between trees: subclasses of
  `NSAccessibilityElement` and AppKit's notifications on macOS; on Linux,
  ATK objects of GObject types registered through purego below the
  accessible of a `GtkGLArea` or `GtkDrawingArea` subclass, with ATK's
  signals, which GTK bridges to AT-SPI; on Windows, UI Automation
  fragments, COM objects with control patterns, answering `WM_GETOBJECT`,
  with UI Automation's events.
  What assistive technology does comes back as `AccessAction` events,
  which the engine performs as the pointer or the keyboard would.
- **The connection.** `content.go` attaches the content to its window
  through `internal/surface.Conn`, which carries the surface and, as
  functions, what the content needs of the app (the clipboard, dragging
  the window, the appearance, the room of a hidden title bar's controls,
  opening URLs, context menus), so `ui` imports neither
  `mygo` nor a backend, and an app without native UI links none of it.
  `Window.Update` and `Invalidate` coalesce redraws asked from any goroutine
  into one frame on the main thread. Page methods return `errNoPage` or do
  nothing.
- **Frames.** The engine (`ui/runtime.go`) calls the view to build a
  frame, again (up to three times) when a handler changed the state while
  it built, so the frame shows the outcome; lays it out with flexbox
  (`layout.go`); commits the boxes to the elements' states with the hit
  list in paint order, the focus order and the labels; paints a
  `scene.Scene`; and presents it. Input between frames goes to the states
  of the last frame's elements. An element's identity hashes its parent's
  with its position or `Key`, so focus, scroll offsets, editors and
  animations survive rebuilding. Frames happen only when asked: input,
  `Invalidate`, `After`, or `AnimationFrame` while something moves.
- **Context menus** (`ui/menu.go`) open in two frames. A right-click or the
  menu key marks the element, from the states of the last frame, and the
  next frame runs its `ContextMenu` function to collect a `platform.Menu`;
  after the frame, package mygo shows it with `Menu.popup`, posted to the
  main loop, since native menus wait in a loop of their own. A choice
  comes back as the item's place and label, which the next frame's run of
  the function matches to report `Chosen`; text inputs' menus queue their
  editing commands instead.
- **Scenes** are flat lists of operations in device pixels: rounded
  rectangles with borders and linear gradients, shadows (blurred rounded
  rectangles), runs of glyphs, images, and pushed and popped clips.
  Renderers draw the whole scene each frame and retain only textures.
- **Text.** `internal/text` lays out text with the system's own text stack,
  behind a small `engine` interface: DirectWrite on Windows
  (`IDWriteTextLayout`, with an `IDWriteTextRenderer` implemented in Go
  collecting its glyph runs), Core Text on macOS (`CTTypesetter`, with
  `NSFont` for the system font's weights) and Pango on Linux (with
  fontconfig and HarfBuzz underneath, as in GTK). They find the fonts, fall
  back to other fonts for what one lacks, shape, run the bidirectional
  algorithm and break lines; font files stay memory mapped and shared with
  every other process. The package assembles their lines into layouts, with
  its own whitespace trimming, ellipsis, alignment, carets and selection, so
  those behave the same everywhere, and caches layouts by their parameters.
  The engines rasterize glyphs too (`IDWriteGlyphRunAnalysis`,
  `CTFontDrawGlyphs`, cairo), at four subpixel offsets, into a coverage
  atlas, and color glyphs (emoji) into a color atlas; paths drawn with
  `Painter` are masks in the coverage atlas, rasterized by `internal/vec`.
  So are icons: `internal/svg` parses SVG documents (with `encoding/xml`
  and its own small CSS cascade) into nodes of paths, paints and layers,
  and draws them on the CPU with `internal/vec`, compositing gradients,
  group opacity, clip paths and masks in layers of premultiplied pixels;
  `ui.Icon` keeps the coverage of that drawing for each size, which the
  GPU tints like a glyph, and `ui.Image` the colored pixels, as images
  whose textures it updates in place when only the text color changes.
  An atlas logs the rectangles that change, so renderers upload only those.
  DirectWrite methods taking floats are called through purego, which sets
  the floating point registers that `syscall` leaves alone on ARM64.
  Letter spacing and OpenType features go to each engine's own, so lines
  break with them: `IDWriteTextLayout1`'s character spacing and an
  `IDWriteTypography`, Core Text's tracking and a font copied with feature
  settings, Pango's attributes. So do the spans of rich text, which style
  ranges of runes (`text.Span`): DirectWrite's setters over a range, the
  fonts of a Core Text attributed string's ranges, Pango's attributes
  between byte indices. `Params.Spans` holds them encoded as a string, so
  `Params` stays a key of the layout cache. Colors, underlines and
  strikethroughs of spans only paint, by the rune each glyph starts.
  Fontconfig knows no desktop's interface font, so on Linux `system-ui`
  stands for the family of GTK's `gtk-font-name` first
  (`platform.Theme.UIFont`, which package ui gives `SetUIFamily` with each
  change of the appearance).
- **Atlas lifetime.** A mask drawn for the first time goes to the
  transient zone at the bottom of the atlas, which the next frame frees,
  and moves to the lasting zone at the top once another frame draws it, so
  animated shapes never fill the atlas. When an atlas fills up during a
  frame anyway, the engine calls `MakeRoom`, which repacks what the frame
  drew, grows the atlas when that is much of it and forgets the rest, and
  paints the frame again: no frame shows with glyphs missing.
- **Renderers.** `internal/gpu` turns a scene into one instanced quad per
  operation, in batches that share a scissor rectangle and an image, for
  one shader that computes the signed distance to rounded rectangles, Evan
  Wallace's blurred rounded box, gradients, atlas coverage and the
  innermost rounded clip; outer clips are scissor rectangles. Every GPU
  renderer draws these instances:
  - `internal/gpu/d3d11` with a shader compiled to DXBC ahead of time
    (`go generate ./internal/gpu/d3d11` on Windows, with the system's
    `d3dcompiler_47.dll`), so apps carry no shader compiler. The
    generated file records the SHA-256 of the source it came from, line
    endings aside (`gpu.SourceSum`), as Metal's does: bytecode older than
    `shader.hlsl` falls back to compiling that with the same DLL, which
    Windows has, and its test fails. It draws into a flip-model swap
    chain on the surface's window, with WARP when no hardware device
    works;
  - `internal/gpu/metal` with a shader in Metal Shading Language
    compiled into a Metal library ahead of time (`go generate
    ./internal/gpu/metal` on macOS, with Xcode's `metal` tools), which
    spares a first launch the 100 to 150 ms Metal takes to compile the
    source until it has cached it; a library older than `shader.metal`
    falls back to that, and its test fails. It draws into a CAMetalLayer
    it adds to the surface view's layer. Its frames present with the Core
    Animation transaction (`presentsWithTransaction`), so a live resize
    shows no stretched frames, and each frame waits for the GPU to finish
    the last before it updates the textures and the instance buffer the
    last read, so two drawables do rather than the three a layer makes
    while a window animates. Two seconds after the last frame, as the
    driver frees its own memory of frames, a timer shrinks the drawables,
    which frees all but the one shown, until the next frame makes them
    again: an idle window keeps one frame of memory. It also presents
    frames drawn on the CPU without the GPU (`PresentPixels`): it locks
    the next drawable's IOSurface and copies what changed since the frame
    drawn in memory that drawable holds (it keeps the damage of the last
    four, and whether the GPU drew into a drawable since), all of it after
    the GPU did, and presents it, with no command buffer: the driver
    allocates its 32 to 44 MB only for frames that run on the GPU, and the
    CPU draws a small change in a fraction of the time the GPU takes to
    start. Its drawables are not `framebufferOnly` for that;
  - `internal/gpu/gl` with the shader in GLSL 3.30 or GLSL ES 3.00, which
    the driver compiles when the renderer starts, since these versions
    have no compiled form every driver takes, and drivers keep what they
    compiled between launches, as Mesa and NVIDIA's do. It draws into
    the framebuffer of the GtkGLArea, which GTK shows. GL functions come
    from libepoxy, as GTK's do. Instances draw from per-instance
    attributes, pointed at each batch's first since OpenGL ES 3.0 has no
    base instance. The area's `create-context` asks GDK for OpenGL 3.3,
    else OpenGL ES 3.0, as GPUs with OpenGL ES alone have, and so does
    the probe below (`glContext`).

  Linux draws with OpenGL only where it runs on a GPU: the backend makes
  one context, on a window that never shows, when the first surface is
  created, and reads its renderer. A software renderer such as Mesa's
  llvmpipe (in virtual machines, in WSL without `GALLIUM_DRIVER=d3d12`)
  redraws every pixel of every frame on the CPU, ten times the CPU
  renderer's work on an animated page; and once a window has a GL context
  GTK composites it with OpenGL, so the choice is made before any surface
  has one; `MYGO_GPU=1` skips it and draws with OpenGL wherever GDK makes a
  context, as the GUI tests of CI do on llvmpipe. The context loads Mesa
  for good, about 20 MB, so the devices answer first where they can: no
  probe without NVIDIA's devices or a render node of a DRM driver with 3D
  (simpledrm, bochs, VirtualBox's or Hyper-V's only show what the CPU
  drew), nor on WSL's device without `GALLIUM_DRIVER=d3d12`. A GtkGLArea shows only
  what OpenGL draws into it: when its GL renderer fails all the same, the
  frames drawn in memory go through OpenGL too (`gl.Presenter` uploads
  them and blits them into the area's framebuffer).

  `internal/raster` draws the same scene with the same formulas on the CPU,
  solid spans inside shapes and only the edges of shadows computed, and
  redraws only what differs from the last scene (`raster.Renderer`): it is
  the renderer of tests, of `MYGO_GPU=0` and of Linux without a GPU, and
  the one a window falls back to when its GPU renderer fails. Frames that
  are not the surface's (a capture before the first frame) are kept, not
  drawn: OpenGL's context is current only in the surface's.

  Where the GPU renderer presents frames drawn in memory (Metal's), the
  window host draws on the CPU the frames that change little, measuring
  first what `raster.Renderer` would redraw (`Changes`): a frame after a
  pause of 50 ms or more, unless it redraws more than 8 million pixels,
  and in a burst of frames one that redraws at most a sixteenth of the
  window: clocks, typing, the pointer over a button, a progress bar.
  Scrolling, resizing and animations of much of the window draw on the
  GPU, and the next frame after a pause catches up on the CPU. Once the
  GPU has drawn alone for a second, the host frees the CPU's frame. The
  gallery, which updates once a second, takes 0.2 to 0.4% of a core and
  67 to 77 MB on macOS this way, against 0.4 to 0.5% and 110 to 116 MB on
  the GPU alone.

  A GPU renderer that fails (a driver reset, a GPU unplugged, sleep) is
  released and another made in the same frame (`ui/window.go`): the GPU
  may be back already, and on Windows WARP stands in, presenting through a
  swap chain, as GDI cannot show frames once a flip-model swap chain has
  drawn into a window. Without one, frames are drawn in memory, and a
  frame tries again after a wait that doubles from one second to half a
  minute; a renderer drawing in software in place of a GPU the window had
  tries for it the same way. A window without a GPU at its first frame
  draws in memory for good.
- **Tests.** `ui.Tester` runs views against a host in memory
  (`ui/headless.go`) with the CPU renderer; the fake backend's surface lets
  the core's tests drive content windows through `package mygo`.
  `BenchmarkFrame` in `ui` measures a frame of a large window on the CPU.
  The GPU renderers' tests draw `gputest.Scene` and compare it with the
  CPU renderer's drawing: on Windows in a hidden window, on macOS into an
  offscreen texture, on Linux into a framebuffer of a context EGL makes
  without a window (Mesa's surfaceless platform, llvmpipe without a GPU),
  once with OpenGL 3.3 and once with OpenGL ES 3.0.

A new widget composes elements (`ui/widgets.go`), keeps what it needs from
frame to frame with `Local` or in `state`, declares its interaction with
element flags (`flagClickable`, `flagFocusable`, …), paints in `Draw`, and
gets a test with `Tester`. A new GPU renderer draws the instances of
`gpu.Builder` with a port of the shader, syncs atlases with
`Atlas.Changes`, is created in `ui/gpu_<os>.go` behind `gpuRenderer`, and
has a test that compares its drawing of `gputest.Scene` with the CPU
renderer's (`gputest.Compare`).

## CLI (`cmd/mygo`)

- `init` renders `cmd/mygo/template`: a Go module and a TypeScript frontend
  built with Vite, side by side at the project root like the examples, draws
  a default icon at `resources/icon.png`, fetches modules, installs the
  JavaScript dependencies with Bun and generates the client. package.json
  runs the CLI from mygo-cli (`bun run dev`, `bun run build`), or with
  `go run github.com/egoist/mygo/cmd/mygo` for `-mygo <checkout>`, whose
  go.mod replaces the module with the checkout. mygo.config.ts imports
  `defineConfig` from mygo-cli, or from the checkout's `packages/cli` by a
  relative path between real locations, and sets `devUrl`, `devCommand` and
  `buildCommand` (the `dev:web` and `build:web` scripts, which run Vite and
  never mygo), `frontendDist` (Vite's `dist`) and `out` (`build`, so the two
  do not meet); `vite.config.ts` pins the dev server to the port of `devUrl`
  and does not watch the development app and builds. Names reach the
  templates escaped for their language (`json`, `printf "%q"`, `html`).
  `-template native` renders `cmd/mygo/template/native` instead (the
  frontend's is `template/web`): an app of native UI with a test of its
  view and a `mygo.json`, whose module has the CLI as a tool (`go get
  -tool`, or a `tool` line beside the `replace` of a checkout), for
  `go tool mygo dev` and `build`; no Bun. Without a frontend in the
  configuration, `bindings` stays empty and the CLI writes no client.
- The configuration is `mygo.config.ts`, or `mygo.json` (`config.go`,
  `config_ts.go`). For the former, Bun, else Node.js 22.6 or later (with
  `--experimental-strip-types` before 22.18 and 23.6), runs a loader that
  imports it, awaits its default export or calls it with `{ command }`, and
  writes JSON to a temporary file; either way the JSON goes through the same
  checks. Errors name the file in use. `defineConfig` and the types of the
  configuration come from `packages/cli/index.d.ts`; `TestConfigTypes`
  keeps its interfaces in step with the `Config` struct.
- `generate` builds the app for the host and runs it in generate mode
  (`MYGO_GENERATE`; `RequestSingleInstanceLock` then returns true at once).
- `dev` (`dev.go`, `watch.go`) writes the client as `generate` does (the
  frontend imports it), runs `devCommand` in the project directory, waits
  for `devUrl` to answer and runs a development build, pointed at it
  with `MYGO_DEV_URL` (or at `frontendDist` on disk without `devUrl`). The
  build is packaged like a release: on macOS a bundle named
  "<name> Dev" with identifier "<id>.dev" in `.mygo/dev/<goos>-<goarch>`, so
  bundle-only features (notifications, URL schemes) work and its data stays
  apart from the production app's. Builds are assembled in a staging
  directory and renamed into place, so the running build keeps its files.
  - *Ready handshake.* The CLI listens on a Unix socket and passes it in
    `MYGO_READY_SOCKET`; the core connects once the first window is ready to
    show or failed to load, right after launch when there is no window, and
    at most 5 s after launch otherwise (`dev.go`). Nothing happens in
    production builds.
  - *Reload.* A change (polling every 250 ms, debounced) rebuilds;
    when the executable, Info.plist and icon are unchanged nothing restarts.
    Otherwise the running build is sent SIGTERM (quit sequence), then
    SIGKILL after 3 s, and the new one starts once it has exited: builds
    never overlap, so the single-instance lock, the web view's profile and
    the app's files are free. Only the app gets the SIGTERM, which lets it
    end the processes it started; those left after it exits are killed. A
    build that fails to compile leaves the old one running; one that fails
    to start or get ready within 20 s leaves none until the next change.
  - *Watching.* Exactly what the build reads, from `go list -deps` after
    every build: the directories of the compiled packages outside GOROOT and
    the module cache (so local `replace` modules too), embedded files,
    go.mod/go.sum, the configuration, the icon and the resources, but not
    the platform directories of other platforms. Frontend sources
    are the dev server's business and never rebuild the app. A build keeps
    the watcher's baseline unless it changed what is watched, so edits made
    during a build trigger another one.
  - Quitting the app ends `mygo dev`; a crash waits for the next change.
- `build` generates the client, runs `buildCommand`, then compiles each
  target with `-trimpath -ldflags "-s -w -X …production=1"` (`-H=windowsgui`
  on Windows) into a staging directory, so a failed build keeps the previous
  artifacts. `frontendDist` is embedded without touching the project
  (`embed.go`): `go build -overlay` adds a generated `mygo_frontend_gen.go`
  to the main package, with `//go:embed all:mygo_frontend` and a call to
  `SetFrontend`, and maps every `frontendDist` file into that virtual
  directory, so the frontend may live anywhere. macOS targets become `.app`
  bundles (Info.plist, `.icns` rendered in pure Go), signed with
  `macos.signingIdentity` (hardened runtime and timestamp for real
  identities, ad hoc by default); `darwin/universal` combines both
  architectures with a pure-Go fat-binary writer. Linux gets a `.desktop`
  entry and icon. The production flag makes `IsDev` false, which disables
  the web inspector by default.
- *Resources* (`resources.go`), as in quickgui: the contents of the
  project's `resources/` directory, plus the files and directories listed
  in `resources` in the configuration under their base names, are copied into
  `Contents/Resources` of macOS bundles and next to the executable on
  Linux and Windows, by `build` and `dev` alike; apps find them with
  `App.Path(PathResources)` (under `go run`, `./resources`). The platform
  directories of `resources/`, named `<goos>` or `<goos>-<goarch>`
  (`platformDir`; names other tools use, such as `darwin-x64`, are
  errors), ship with the apps of that target only, their entries merged
  with the shared ones: `merger` groups what goes to one path, ignoring
  case, and only descends into directories that several sources share, so
  a whole tree from one source stays one entry. For `darwin/universal`,
  `darwin-arm64` and `darwin-amd64` are walked as pairs whose names must
  match: identical files and links ship once, an arm64 and an x86_64
  Mach-O file become a universal binary (`writeUniversal`) when copied,
  anything else fails. Names starting with a dot are skipped; the entries
  of these directories and listed paths are followed when they are links,
  links inside them are copied as links, permissions are kept. Installed
  paths must be unique ignoring case (listed resources never merge), and
  top-level names must not replace the packaging's own files
  (`AppIcon.icns`, the executable, the `.desktop` entry). Code
  among them is signed before the app (`signNestedCode`), since
  `codesign --deep` only covers code directories and notarization rejects
  unsigned code: Mach-O files, then the bundles holding them, deepest
  first, as a bundle's signature seals what it holds (and signing its main
  executable signs the whole bundle). A real identity signs all of it,
  keeping the entitlements of each (`--preserve-metadata`) unless
  `macos.helperEntitlements` names others; ad hoc signing only signs code
  without an `LC_CODE_SIGNATURE`, and the bundles around it, so vendors'
  signatures stay. Windows builds sign the PE images among the resources
  that have no certificate table, with the app's certificate or command.
  `resources/icon.png` is the default icon. Each platform's output
  directory is assembled in a staging directory that replaces it whole, so
  removed resources do not linger; development builds on Linux and Windows
  remove what the previous build placed and this one lacks.
- Packages for the other platforms. Windows gets "<name> Setup
  <version>.exe", made with NSIS (`nsis.go`): a per-user install in
  `%LOCALAPPDATA%\Programs\<name>`, where the updater can write, a Start
  menu shortcut, a desktop shortcut that the finish page's second check
  box (MUI's "show readme" one) creates, and an uninstaller registered
  under `HKCU\…\Uninstall\<identifier>`; `/S /D=<dir>` installs
  silently, without the desktop shortcut.
  `makensis` comes from an installation of NSIS or, on Windows, where NSIS
  is rarely installed, from the official zip of the release `nsisRelease`
  pins, which the CLI downloads once, checks against its SHA-256 and
  unpacks into `<user cache>/mygo`, as Tauri does. It tries the copies of
  the zip in `nsisRelease.urls` in order, until one answers with the right
  SHA-256: the asset of this repository's `nsis-<version>` release, then
  SourceForge, where NSIS publishes it. SourceForge's download host has
  been down for hours at a time, and its mirrors then redirect to it, so
  it is not the only source; a host that sends no response headers within
  30 s gives way to the next. Updating NSIS means publishing the copy (see
  [Releasing](#releasing)). Other systems skip the
  installer without NSIS: its zip holds Windows programs only. A signed
  app gets a signed uninstaller too, as with Tauri: `!uninstfinalize`
  (NSIS 3.08 and later) makes makensis run `mygo sign-uninstaller` on the
  uninstaller it generates, before it puts it into the installer, and the
  CLI signs it as it signs the app. The executable and the Windows
  configuration reach it through the environment (`MYGO_SIGNER`,
  `MYGO_SIGN_SETTINGS`), since NSIS reads `$` in the script as its own
  syntax. Linux
  gets a Debian package written in pure Go (`deb.go`), whose maintainer
  is `linux.maintainer`, else the `author` of package.json, else the name
  of the app: the app in `/opt/<name>`, a `/usr/bin` link named
  `linux.command` when there is one, the desktop entry (categories,
  comment, URL schemes; `Exec` is the app's path) and hicolor icons,
  depending on GTK 3 and WebKitGTK 4.1. Packages hold the same files as
  the update archive; apps installed by a package manager do not update
  themselves (`Updater.Enabled` checks that the app can write where it is
  installed). Every Linux build also gets `install.sh`
  (`installscript.go`), a POSIX sh script, the same for every
  architecture, that installs the archive for the user in
  `~/.local/<name>.app` (which updates can replace), links
  `~/.local/bin/<linux.command>` when there is one and no other program
  is there (and removes a `~/.local/bin/<name>` link of its own, which
  earlier scripts made), and registers the app's `<name>.desktop`, with
  absolute `Exec` and `Icon` paths, and `<name>.xml`, the shared-mime-info
  package of the types it defines, under `$XDG_DATA_HOME`. It then warns
  when `ldconfig -p` lists no WebKitGTK (4.1 or 4.0, the libraries
  `internal/linux` loads), with the command that installs it on the
  distribution `/etc/os-release` names, and stays silent when it cannot
  tell (no `ldconfig`, or a cache without libc, as on NixOS or musl).
  `Update.Install` registers them again from the new version
  (`update.RefreshDesktopEntry`, the same rewrite in Go, which a test
  compares with the script's) when the user's entry runs the app it
  updated. It takes the archive it is given, else the one of its
  version next to it, else, with updates, the `url` of the target's
  manifest, read with sed from the indented JSON `mygo build` writes.
  `--uninstall` removes only what points into its install, including the
  URL handler entry the app registers.
- On a macOS host, macOS targets also get "<name> <version>.dmg"
  (`dmg.go`): `hdiutil` creates a writable HFS+ image from the app, the CLI
  adds the `/Applications` link, the volume icon and a `.DS_Store` written in
  pure Go (`dsstore.go`, byte-identical to dmgbuild's `ds_store` package) that
  lays out the Finder window, then `hdiutil convert` compresses it with LZMA.
  No AppleScript or Finder automation is involved, so it works headless and
  in CI. The image is signed with a real identity and, with `macos.notarize`,
  notarized with `notarytool` and stapled.

- `keygen` writes the update signing keys (see Updates above).

Configuration lives in an optional `mygo.config.ts` or `mygo.json`
(`cmd/mygo/config.go`): app
metadata (the icon defaults to `resources/icon.png`), extra `resources`,
the frontend (`devUrl`, `devCommand`, `buildCommand`, `frontendDist`,
`bindings`) and the `macos` section (minimum system version, signing
identity, entitlements of the app and of helpers, DMG title, notarization
profile).

## Testing

| suite | command | covers |
|---|---|---|
| core | `go test .` | lifecycle, quit, IPC, channels, events, Eval, protocol, frontend URLs and serving, menus, trust, single instance, dev ready signal (fake backend); `go test -run '^$' -bench .` measures the Go side of IPC and custom schemes |
| generator | `go test ./internal/tsgen` | TS output, json/v2 rules, source lookup; type-checks the output with `tsc` when `bun install` was run |
| CLI | `go test ./cmd/mygo` | config, Info.plist, icons, universal binaries, template, dev launch/ready/stop (the test binary plays the app), watcher and `go list` inputs, resources (platform directories, universal pairs, staging, conflicts, dev placement; builds for every OS), frontend embedding (compiles an app with the overlay), `.DS_Store` against a dmgbuild golden file, a real DMG (`hdiutil`); builds and tools are skipped with `-short` |
| runtime | `bun run test` | the injected runtime, `mygo-runtime` and the plugins' packages (against a fake Go side on the real runtime, `plugins/fake-go.ts`) |
| plugins | `go test ./plugins/...` | the fetch plugin against `httptest` servers, the WebSocket client against a test server (ordering, fragments, pings, closing handshakes) |
| native UI | `go test ./ui ./internal/text ./internal/scene ./internal/raster ./internal/svg ./internal/gpu/...` | the GPU renderers against the CPU renderer (Direct3D on Windows, Metal on macOS, OpenGL on Linux); views through `Tester`: input, focus, editing, lists, overlays, frames that fill the glyph atlas; text layout and caret geometry; atlas zones and repacking; the CPU renderer against its formulas; SVG parsing and drawing, with `FuzzParse`; `go test -run '^$' -bench . ./ui` times a frame |
| GUI | `MYGO_E2E=1 go test ./internal/e2e` | the real backend: IPC, channels, protocol, Eval, geometry, capture, menus, window.open, native UI (frames, clicks, input methods replacing typed text, file drops, assistive technology reading and acting; on macOS typing, skipped while an input method is selected, and composing; on Linux with `MYGO_GPU=1`, what OpenGL drew in the GtkGLArea); on Windows too (a GitHub Actions `windows-latest` runner has WebView2) |

The XDG variables let the URL scheme test check that GLib opens the scheme
with the handler it registered; without them it writes to temporary
directories, which GLib does not see.

`internal/fake` runs its loop on the goroutine that calls `Run` and records
evaluated scripts, so tests can assert on exactly what the page would
receive. The unit tests run `App.Run` on the main goroutine from `TestMain`,
like a real program.

Linux GUI tests run in a container, since no cgo means the test binary
cross-compiles:

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -o e2e.test ./internal/e2e
docker run --rm -v "$PWD:/work" -w /work -e MYGO_E2E=1 \
  -e XDG_DATA_HOME=/tmp/xdg-data -e XDG_CONFIG_HOME=/tmp/xdg-config \
  <image with libwebkit2gtk-4.1-0, xvfb, dbus> \
  dbus-run-session -- xvfb-run -a ./e2e.test
```

`TestGlobalShortcutPortal` binds global shortcuts through the desktop portal
as on Wayland, and skips without one. KDE's portal works on X11, so an image
that adds `xdg-desktop-portal`, `xdg-desktop-portal-kde` and `kglobalacceld`
(Plasma 6; `libkf5globalaccel-bin` for Plasma 5, which D-Bus activates) runs
it for real: start `xvfb-run` outside `dbus-run-session` so the portals
D-Bus activates get `DISPLAY`, set `XDG_CURRENT_DESKTOP=KDE`, start
`/usr/lib/*/libexec/kglobalacceld` before the tests, and install
`$XDG_DATA_HOME/applications/e2e.test.desktop` so that the portal accepts
the test binary's app ID. It passes on Debian 13 (portal 1.20, Plasma 6.3)
and Debian 12 (portal 1.16, Plasma 5.27); Ubuntu 24.04 (portal 1.18, Plasma
5.27) binds no shortcuts for any app.

## Releasing

The Go module, the CLI and the npm packages share one version:

```sh
bun scripts/version.ts 0.2.0   # mygo.Version, the CLI and every package.json
git commit -am "Release 0.2.0" && git tag v0.2.0 && git push origin main v0.2.0
```

The tag starts `.github/workflows/release.yml`, which checks that the tag
matches the versions (`bun scripts/version.ts --check`) and runs
`bun scripts/publish.ts --provenance`: it builds the CLI's binaries and
publishes mygo-runtime and the platform packages, then mygo-cli once npm
serves them to package managers, skipping versions already on npm,
prereleases under the `next` dist-tag. npm can take minutes to serve new
packages, and a package manager installs mygo-cli without a platform
package it cannot fetch; bun then keeps that package out while its lockfile
lacks it, even with `--force`. The workflow then asks the Go module proxy
for the tag and creates the GitHub release.

It does not run the tests again: `ci.yml` runs them on every push to main,
for every platform, GUI tests included, so tag a commit whose CI passed.
Its Windows job keeps the NSIS that the installer tests download in the
Actions cache, under a key of `cmd/mygo/nsis.go`.

The NSIS that `mygo build` downloads (`nsisRelease` in `cmd/mygo/nsis.go`)
has a copy on a release of this repository, which a new version of NSIS
needs before a MyGo release that pins it. Download the official zip,
check that its SHA-256 is the one `nsisRelease` pins, and publish it,
without making the release the latest:

```sh
curl -fLO https://downloads.sourceforge.net/project/nsis/NSIS%203/3.13/nsis-3.13.zip
sha256sum nsis-3.13.zip
gh release create nsis-3.13 nsis-3.13.zip --latest=false --title "NSIS 3.13" \
  --notes "The official NSIS 3.13 zip, which mygo build downloads on Windows."
```

The tag starts no workflow, and the Go module proxy ignores it.

npm authenticates the workflow as a trusted publisher of each package
(`release.yml` of this repository, set in the package's settings on npm),
which npm allows only for packages that exist: the first release uses an
`NPM_TOKEN` secret of the repository instead. `bun scripts/publish.ts
--dry-run` shows what would be published.

## Adding a feature

1. **Design the public API first** in `package mygo`: typed, goroutine-safe,
   documented, with sensible zero values. Keep policy (defaults, validation,
   state machines) here.
2. **Extend `internal/platform`** with the smallest mechanism the backends
   need: main-thread only, synchronous, or with a callback that runs on the
   main thread exactly once.
3. **Implement it in every backend**: `darwin`, `linux`, `windows`, `fake`
   and `unsupported` (return `platform.ErrUnsupported` or a zero value).
   Create callbacks once, never per call.
4. **Wire the core**: hop with `onMain`/`onMainValue`, or `postMain` + `await`
   + `deliver` for asynchronous native results. Start with `needsApp` when
   the call needs the running app, or keep a setting until `Run` applies it
   (see the threading model).
5. **Test**: unit test through `internal/fake`, a GUI test in `internal/e2e`
   when behavior depends on the toolkit, and run the Linux GUI tests in the
   container.
6. **If the page runtime changes**, edit `packages/bridge` or
   `packages/runtime`, run `bun run test`, `bun run typecheck` and
   `bun run build`, and commit `internal/bridge/bridge.js`. If the generated client changes, update
   `internal/tsgen/generate.go` and its tests.
7. **Document** the behavior in the Go doc comments and platform
   differences in the README.

## Platform differences

| feature | macOS | Linux | Windows |
|---|---|---|---|
| menu bar | application menu bar, default menu installed | per-window GTK menu bar, none by default | per-window Win32 menu bar, none by default |
| auto-hide menu bar | ignored | the bar widget hides; `can-activate-accel` keeps its shortcuts; Alt alone or F10 show it and open its first menu until it deactivates | the menu is attached only for the `SC_KEYMENU` menu loop that Alt alone or F10 start; shortcuts come from the webview |
| tray | NSStatusItem, click events | AppIndicator (menu only, no click events) | notification area icon, click events |
| global shortcuts | Carbon hot keys | X11: `XGrabKey` on the root window (with Caps/Num Lock variants), key presses from a GDK filter. Wayland: the XDG `GlobalShortcuts` portal (see [Linux](#linux-internallinux)) | `RegisterHotKey` |
| notifications | UserNotifications, packaged apps only | org.freedesktop.Notifications over D-Bus | notification-area balloons (toasts) |
| vibrancy | all materials | ignored | Windows 11 Mica, Acrylic, Tabbed |
| traffic lights, Dock | yes | ignored | ignored |
| hidden title bar | AppKit's traffic lights over a full-size content view | GTK's title buttons in header bars over the page, per `gtk-decoration-layout`; none where the Wayland compositor decorates windows | caption buttons drawn in a layered child window; snap layouts; a top edge that resizes |
| progress bar | Dock tile content view (NSBoxes: NSProgressIndicator does not draw there), app-wide | Unity launcher API over D-Bus (`com.canonical.Unity.LauncherEntry`), app-wide | `ITaskbarList3`, per window |
| badge count | Dock tile label | Unity launcher API count | not shown |
| skip taskbar | ignored | skip-taskbar hint | `ITaskbarList::DeleteTab` (the window style is untouched) |
| FlashFrame | informational Dock bounce | urgency hint | `FlashWindowEx` until focused |
| visible on all workspaces | `NSWindowCollectionBehaviorCanJoinAllSpaces` | `gtk_window_stick` | ignored |
| window icon | ignored | `gtk_window_set_icon` | `WM_SETICON` at the window's DPI |
| URL schemes | Info.plist (`urlSchemes`); `RegisterURLScheme` makes the app the default handler | desktop entry + `mimeapps.list` | `HKCU\Software\Classes` |
| downloads | `shouldPerformDownload`, non-displayable or attachment responses → `WKDownload` delegate | `download-started` / `decide-destination` on the web context; response policy for attachments | `DownloadStarting` (`ICoreWebView2_4`), replacing WebView2's download UI |
| ClearBrowsingData | default `WKWebsiteDataStore`, all types | the web context's website data manager | the WebView2 profile's `ClearBrowsingDataAll` (needs a window) |
| permissions | `WKUIDelegate` media capture (camera, microphone) | `permission-request` (camera, microphone, geolocation, notifications) | `PermissionRequested` (the same four; WebView2 asks about others) |
| file associations | `CFBundleDocumentTypes`; files arrive with `application:openURLs:` | desktop entry `MimeType` (`%U`), a shared-mime-info package in the .deb for types the app defines | ProgIDs and `OpenWithProgids` written by the installer |
| Dock menu | `applicationDockMenu:` | ignored | ignored |
| PrintToPDF | `printOperationWithPrintInfo:` save job (`NSJobSavingURL`), fit to width | `WebKitPrintOperation` to GTK's "Print to File" | DevTools `Page.printToPDF` |
| power events | NSWorkspace sleep/wake, `com.apple.screenIsLocked` distributed notifications | logind `PrepareForSleep` (system bus), screen saver `ActiveChanged` (GNOME, freedesktop) | `WM_POWERBROADCAST`, `WM_WTSSESSION_CHANGE` |
| KeepAwake | `NSProcessInfo` activity (shows in `pmset -g assertions`) | XDG portal `Inhibit`, else `org.freedesktop.ScreenSaver.Inhibit` | `PowerCreateRequest` |
| IsOnBattery, IdleTime | IOKit power sources, `CGEventSourceSecondsSinceLastEventType` | `/sys/class/power_supply`; Mutter idle monitor or `GetSessionIdleTime` | `GetSystemPowerStatus`, `GetLastInputInfo` |
| window position | honored | ignored by Wayland compositors | honored |
| resize borders without a title bar | the window's own | the outer 5 px of the page | invisible, outside the window; along the top of a hidden title bar, a child window |
| content protection, click-through | yes | ignored | yes |
| custom scheme origin | `<scheme>://localhost` | `<scheme>://localhost` | `http://<scheme>.localhost` (the page's `location`) |
| window.open | keeps the opener | independent window | independent window |
| native UI surface | layer-backed NSView, frames from `CADisplayLink` (a timer at the display's rate before macOS 14), input methods through NSTextInputClient | GtkGLArea (GtkDrawingArea without a GPU), GtkIMMulticontext | `MyGoSurface` child window, IMM32 |
| native UI file drops | NSDraggingDestination | GTK drag destination (`text/uri-list`) | OLE `IDropTarget` |
| native UI accessibility | `NSAccessibilityElement` subclasses | ATK objects (GObject types registered through purego), bridged to AT-SPI by GTK | UI Automation fragments (COM objects; assembly thunks for the methods taking doubles) |
| native UI rendering | Metal, into a CAMetalLayer presenting with the Core Animation transaction | OpenGL 3.3 or ES 3.0 in the GtkGLArea's render signal; on the CPU, painted with cairo, where OpenGL runs on the CPU | Direct3D 11 (WARP without a GPU), flip-model swap chain |
