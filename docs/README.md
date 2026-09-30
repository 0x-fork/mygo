# MyGo documentation

MyGo builds desktop apps with Go and a web frontend. Windows show your pages
in the webview of the operating system (WKWebView on macOS, WebKitGTK on
Linux, WebView2 on Windows) instead of a bundled browser, so an app is a
single Go program of a few megabytes. The frontend calls your Go code
through a TypeScript client that MyGo generates from it, with the types of
your Go structs and the documentation of your methods.

```go
type Greeter struct{}

// Greet returns a greeting.
func (Greeter) Greet(name string) string { return "Hello, " + name }

func main() {
	mygo.Bind(Greeter{})
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "Hello", URL: "/"})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
```

```ts
import { Greeter } from "./mygo"; // generated

document.body.textContent = await Greeter.greet("Ada");
```

## Guides

- [Getting started](getting-started.md): install the tools, then create,
  develop and build an app.
- [Calling Go from the frontend](bindings.md): bound services, channels,
  typed events and the generated TypeScript client.
- [Plugins](plugins.md): the official fetch and WebSocket plugins, which
  run in Go, and writing your own.
- [The frontend](frontend.md): how pages load during development and in
  builds, the `mygo-runtime` package, custom protocols, custom title bars
  and dropped files.
- [Windows](windows.md): creating and arranging windows, their events, and
  what they do with their pages: navigation, downloads, permissions,
  printing.
- [The application](app.md): the lifecycle, quitting, a single instance,
  deep links, file associations, starting at login and well-known
  directories.
- [Menus and the tray](menus.md): application, context and Dock menus,
  keyboard shortcuts and tray icons.
- [Native APIs](native.md): dialogs, notifications, the clipboard, the
  shell, displays, dark mode, power and global shortcuts.
- [Building and distributing](distribution.md): packaged apps for macOS,
  Windows and Linux, signing, installers and disk images.
- [Auto-updates](updates.md): signed updates from GitHub releases or your
  own server, and an update window in the manner of Sparkle.

## Reference

- [Configuration](configuration.md): `mygo.config.ts`, or `mygo.json`.
- [The mygo CLI](cli.md): its commands and flags.
- The Go API: `go doc -all github.com/egoist/mygo`, with the documentation
  of every type and method.
- [Architecture](architecture.md): how MyGo works inside, for contributors,
  with a table of the differences between platforms.

## Requirements

To develop apps you need [Go](https://go.dev/dl/) 1.27 or later and, for
the frontend tooling of new projects, [Bun](https://bun.sh). MyGo uses no
cgo, so there is no C toolchain to install, and any machine can compile the
apps of every platform; signing and disk images of macOS apps need a Mac.

Apps run on:

| Platform | Needs |
|---|---|
| macOS 12 or later | nothing: WKWebView is part of macOS |
| Linux (x64, arm64) | GTK 3 and WebKitGTK 4.1 (or 4.0): `libwebkit2gtk-4.1-0` on Debian and Ubuntu, `webkit2gtk4.1` on Fedora. Tray icons also need `libayatana-appindicator3`. |
| Windows 10 and 11 (x64, arm64) | the [WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/), which Windows 11 includes |

`mygo doctor` checks a development machine.
