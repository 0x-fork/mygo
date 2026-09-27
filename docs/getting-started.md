# Getting started

## Install

Install [Go](https://go.dev/dl/) 1.27 or later and [Bun](https://bun.sh).
On Linux, also install GTK 3 and WebKitGTK 4.1, e.g.
`sudo apt install libwebkit2gtk-4.1-0` on Debian and Ubuntu; on Windows,
the [WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/)
unless you run Windows 11, which includes it.

The `mygo` command line tool creates, runs and packages apps. New projects
depend on it through npm, as the `mygo-cli` package, so you can create one
without installing anything else:

```sh
bunx mygo-cli init my-app      # or: npx mygo-cli init my-app
```

You can also install the CLI with Go, which puts `mygo` on your `PATH`:

```sh
go install github.com/egoist/mygo/cmd/mygo@latest
mygo init my-app
```

`mygo doctor` checks that the machine has what MyGo needs.

## The project

`mygo init my-app` creates a Go module and a TypeScript frontend built with
[Vite](https://vite.dev), side by side, and installs their dependencies:

```
my-app/
├── main.go          the app: its windows and the Go code the page calls
├── go.mod
├── mygo.config.ts   the app's name, identifier and version, and how to build it
├── package.json     the scripts, and the frontend's dependencies
├── index.html       the page
├── src/
│   ├── main.ts      the page's code
│   ├── style.css
│   └── mygo.ts      the typed client of the Go code, generated
├── vite.config.ts
├── tsconfig.json
└── resources/
    └── icon.png     the app icon, a 1024×1024 PNG
```

Builds go to `dist/` (the frontend) and `build/` (the packaged apps), and
the development app to `.mygo/`; `.gitignore` leaves them out. See
[configuration](configuration.md) for the fields of `mygo.config.ts`.

## Develop

```sh
cd my-app
bun run dev
```

`bun run dev` runs `mygo dev`. It starts the Vite dev server, builds a
development version of the app, which loads its pages from the dev server,
and starts it:

- Edit `src/main.ts` or `src/style.css` and Vite updates the page right
  away.
- Edit a `.go` file, `mygo.config.ts`, the icon or a resource and mygo dev
  rebuilds the app, regenerates `src/mygo.ts` and restarts the app. A build
  that fails, or crashes on start, keeps the previous one running.

Quit the app, or press Ctrl+C, to stop. Development builds have the web
inspector: right-click the page and choose Inspect Element (Inspect on
Windows), or call `win.OpenDevTools()`.

On macOS the development app is a real app bundle, `My App Dev` with the
identifier of the app plus `.dev`, so that it keeps its data, preferences
and permissions apart from the installed app.

## Call Go from the page

`main.go` binds a Go value, whose exported methods the page can call:

```go
// Greeter is callable from the frontend: `mygo generate` turns its methods
// into typed TypeScript functions in src/mygo.ts.
type Greeter struct{}

// Greet returns a greeting for name.
func (Greeter) Greet(name string) string {
	if name == "" {
		name = "stranger"
	}
	return "Hello, " + name + "! This message comes from Go."
}

// Tick is sent to the page every second.
var Tick = mygo.NewEvent[time.Time]("tick")

func main() {
	mygo.Bind(Greeter{})
	// ...
}
```

`src/mygo.ts`, which mygo dev keeps up to date, turns them into typed
functions and events:

```ts
import { Greeter, events } from "./mygo";

const greeting = await Greeter.greet("Ada"); // Promise<string>
events.tick.on((time) => console.log(time)); // time: string
```

Add a method to `Greeter`, save, and it is there to call once the app has
restarted. [Calling Go from the frontend](bindings.md) covers what methods
can take and return, errors, events and security.

## Build

```sh
bun run build
```

`bun run build` runs `mygo build`, which builds the frontend with Vite,
compiles the app with the frontend embedded in it, and packages it for the
machine's platform in `build/<os>-<arch>/`:

| Platform | Output |
|---|---|
| macOS | `My App.app`, and a disk image `My App 0.1.0.dmg` |
| Windows | `My App.exe`, and an installer `My App Setup 0.1.0.exe` when [NSIS](https://nsis.sourceforge.io) is installed |
| Linux | the executable `my-app` with its desktop entry and icon, and a `.deb` package when `linux.maintainer` is set in mygo.config.ts |

MyGo needs no cgo, so any machine builds for every platform:

```sh
bun run build -- -platform darwin/universal,windows/amd64,linux/amd64
```

The apps run where they are, but to ship them to users, sign them: see
[Building and distributing](distribution.md).

## Next steps

- [Windows](windows.md) and [the application](app.md), to shape the app.
- [Menus and the tray](menus.md) and the [native APIs](native.md).
- The examples in the repository: `examples/hello` (the smallest app),
  `examples/todo` (typed services and events, persistence, dialogs, menus,
  several windows), `examples/frameless` (a custom title bar) and
  `examples/native` (menus, a tray icon, dialogs, notifications, a global
  shortcut, the clipboard and dark mode).
