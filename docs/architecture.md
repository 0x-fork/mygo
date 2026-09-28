# MyGo architecture

This guide explains how MyGo is put together: the layers, the threading
model, how Go talks to the native toolkits without cgo, how the page and Go
exchange typed messages, and how to extend the framework safely. Read it
before changing anything under `internal/`.

## Goals and constraints

- **Low overhead.** A hello-world app is a ~7 MB binary with a ~35 MB
  physical footprint on macOS (mostly AppKit/WebKit) and idles at 0% CPU.
  Nothing polls: all work is driven by native events or explicit wake-ups.
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
├── ipc.go              Bind/BindAs, method calls, Event[T], CallerWindow
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
│   ├── tsgen/          TypeScript client generator
│   ├── accelerator/    parses "CmdOrCtrl+Shift+K"
│   └── e2e/            GUI tests against the real backend (MYGO_E2E=1)
├── packages/           Bun workspace (with the examples' frontends):
│   ├── bridge/         the runtime injected into pages (→ internal/bridge/bridge.js)
│   ├── runtime/        mygo-runtime, the npm package apps and generated clients import
│   └── cli/            mygo-cli, the npm package of the CLI, and in npm/ its
│                       per-platform binary packages
├── cmd/mygo/           the CLI: init, generate, dev, build, doctor
├── examples/           hello, todo, frameless, native, vibrancy
└── docs/               the user guides, and this architecture guide
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
   initializes the backend and blocks in the native event loop.
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
     what makes `win.Eval`, `CapturePage` and dialogs usable from event
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

- `ffi.go` `dlopen`s GLib, GObject, GIO, GDK, GTK 3, WebKitGTK 4.1 (4.0),
  JavaScriptCore, libsoup 3 (2.4), cairo, GdkPixbuf and, when installed,
  AppIndicator. Symbols from newer WebKitGTK versions are bound optionally
  and feature-detected (`webkitWebViewCallAsyncJavascriptFunction != nil`).
- Signals are connected with `g_signal_connect_data`, passing the window id as
  user data; `Backend.window(data)` resolves it and ignores closed windows.
  GDK event structs are read at fixed 64-bit offsets (`field[T]`).
- The main thread is the one that called `gtk_init_check`; `IsMainThread`
  compares `gettid`. `Step` is `g_main_context_iteration(NULL, TRUE)` and
  `Wake` is `g_main_context_wakeup`.
- GTK geometry changes are asynchronous: `SetBounds` remembers the requested
  rectangle and `Bounds` reports it until the configure event confirms it.
- GTK gives windows without decorations no resize borders, so the outer
  5 px of a frameless window's page resize it (16 px along the edges from
  a corner resize the corner). The web view's `motion-notify-event` shows
  a resize cursor there, keeping WebKit's cursor to restore, and its
  `button-press-event` calls `gtk_window_begin_resize_drag` with the press,
  as a Wayland compositor requires; neither event reaches WebKit. Nothing
  resizes a maximized or full screen window, and a tiled one only resizes
  at the edges the window manager allows, as with GTK's own decorations.
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
  the executable wins. The environment and each controller are created
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
  floats cannot be passed, so zoom falls back to CSS (`abi_*.go`).
- DPI: the process is per-monitor aware (v2); the backend converts between
  pixels and DIPs with the window's or monitor's DPI. Frameless windows drop
  the caption in `WM_NCCALCSIZE` but keep the side and bottom borders, which
  Windows 10+ draws invisibly outside the window, so they still resize.
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

- Find in page (`find.ts`, `Window.FindInPage`): the bridge walks the
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
protocol. Its `dist/` is committed so the workspace and local projects
(`mygo init --mygo <checkout>` depends on it with `file:`) need no build; the
template depends on `^<version>` from npm, released in step with the Go
module.

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
away. Values are queued in the window's outbox like replies, so they are
batched and stay in order with them.

Flow control keeps a producer faster than the page from piling up
messages: every half MiB of messages, a value asks for an acknowledgment,
which the page sends once it took that value (handled it, or yielded it to
an iterator), and `Send` waits while more than a MiB is unacknowledged. It
never waits on the main thread, which receives the acknowledgments.

### Eval

`Window.Eval(code)` uses the webview's native async-function API
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
servers in development, and origins listed in `WindowOptions.TrustedOrigins`
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
  `<name>-<version>-<target>.tar.gz`, signs its SHA-256, and writes
  `update-<target>.json` with the `## <version>` section of CHANGELOG.md as
  notes. `Updater.Check` fetches the manifest (HTTPS only, loopback HTTP for
  tests) and compares versions semantically; `Update.Install` streams the
  archive next to the app, verifies size and signature, unpacks it (files,
  directories and relative links only) and swaps it in: the bundle is
  renamed on macOS, the entries of the app directory on Linux and Windows,
  where the running executable is renamed away and removed at the next
  launch. `App.Relaunch` then starts the new version from the path the app
  started from. Development builds are never updated. `mygo build -upload`
  publishes to `updates.github` with the `gh` CLI: it creates the release
  `<tagPrefix><version>` as a draft with the changelog section as notes,
  uploads installers and update archives, then the manifests, and leaves
  publishing the draft (which makes the manifests "latest") to the
  developer once every platform is there.
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
  `webkit_web_view_execute_editing_command` on Linux) and reports everything
  else through `AppHandler.MenuItemClicked`; the core toggles checkbox/radio
  state, performs window and view roles and calls `Click`.

macOS gets a default menu bar (App, File, Edit, View, Window), which is what
makes Cmd+C/V/Q work; other platforms get none unless the app sets one.

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
- `dev` (`dev.go`, `watch.go`) runs `devCommand` in the project directory,
  waits for `devUrl` to answer and runs a development build, pointed at it
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
  - *Blue-green reload.* A change (polling every 250 ms, debounced) rebuilds;
    when the executable, Info.plist and icon are unchanged nothing restarts.
    Otherwise the new build is launched and only once it is ready is the old
    one sent SIGTERM (quit sequence), then SIGKILL after 3 s. A build that
    fails to compile, start or get ready within 20 s leaves the old one
    running. A new instance takes over the single-instance lock, and the old
    instance only removes the lock socket if it is still its own.
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
  <version>.exe" when NSIS (`makensis`) is installed (`nsis.go`): a
  per-user install in `%LOCALAPPDATA%\Programs\<name>`, where the updater
  can write, a Start menu shortcut and an uninstaller registered under
  `HKCU\…\Uninstall\<identifier>`; `/S /D=<dir>` installs silently. Linux
  gets a Debian package written in pure Go (`deb.go`) when
  `linux.maintainer` is set: the app in `/opt/<name>`, a `/usr/bin` link,
  the desktop entry (categories, comment, URL schemes) and hicolor icons,
  depending on GTK 3 and WebKitGTK 4.1. Packages hold the same files as
  the update archive; apps installed by a package manager do not update
  themselves (`Updater.Enabled` checks that the app can write where it is
  installed).
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
| core | `go test .` | lifecycle, quit, IPC, channels, events, Eval, protocol, frontend URLs and serving, menus, trust, single instance and its dev handover, dev ready signal (fake backend); `go test -run '^$' -bench .` measures the Go side of IPC and custom schemes |
| generator | `go test ./internal/tsgen` | TS output, json/v2 rules, source lookup; type-checks the output with `tsc` when `bun install` was run |
| CLI | `go test ./cmd/mygo` | config, Info.plist, icons, universal binaries, template, dev launch/ready/stop (the test binary plays the app), watcher and `go list` inputs, resources (platform directories, universal pairs, staging, conflicts, dev placement; builds for every OS), frontend embedding (compiles an app with the overlay), `.DS_Store` against a dmgbuild golden file, a real DMG (`hdiutil`); builds and tools are skipped with `-short` |
| runtime | `bun run test` | the injected runtime and `mygo-runtime` |
| GUI | `MYGO_E2E=1 go test ./internal/e2e` | the real backend: IPC, channels, protocol, Eval, geometry, capture, menus, window.open; on Windows too (a GitHub Actions `windows-latest` runner has WebView2) |

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
   + `deliver` for asynchronous native results.
5. **Test**: unit test through `internal/fake`, a GUI test in `internal/e2e`
   when behavior depends on the toolkit, and run the Linux GUI tests in the
   container.
6. **If the page runtime changes**, edit `packages/bridge` or
   `packages/runtime`, run `bun run test`, `bun run typecheck` and
   `bun run build`, and commit `internal/bridge/bridge.js` and
   `packages/runtime/dist`. If the generated client changes, update
   `internal/tsgen/generate.go` and its tests.
7. **Document** the behavior in the Go doc comments and platform
   differences in the README.

## Platform differences

| feature | macOS | Linux | Windows |
|---|---|---|---|
| menu bar | application menu bar, default menu installed | per-window GTK menu bar, none by default | per-window Win32 menu bar, none by default |
| tray | NSStatusItem, click events | AppIndicator (menu only, no click events) | notification area icon, click events |
| global shortcuts | Carbon hot keys | X11: `XGrabKey` on the root window (with Caps/Num Lock variants), key presses from a GDK filter. Wayland: the XDG `GlobalShortcuts` portal (see [Linux](#linux-internallinux)) | `RegisterHotKey` |
| notifications | UserNotifications, packaged apps only | org.freedesktop.Notifications over D-Bus | notification-area balloons (toasts) |
| vibrancy | all materials | ignored | Windows 11 Mica, Acrylic, Tabbed |
| traffic lights, Dock | yes | ignored | ignored |
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
| frameless resize borders | the window's own | the outer 5 px of the page | invisible, outside the window |
| content protection, click-through | yes | ignored | yes |
| custom scheme origin | `<scheme>://localhost` | `<scheme>://localhost` | `http://<scheme>.localhost` (the page's `location`) |
| window.open | keeps the opener | independent window | independent window |
