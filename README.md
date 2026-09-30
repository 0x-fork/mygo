# MyGo

Desktop apps with Go and a web frontend, on the system webview.

MyGo apps show their UI in the webview the OS already has: WKWebView on
macOS, WebKitGTK on Linux, WebView2 on Windows. An app is a single Go
binary of a few megabytes, focused on low memory and CPU use.

- **Pure Go, no cgo**: build for every platform from any machine.
- **Typed IPC**: bind Go services, stream values through channels and send
  typed events; the TypeScript client is generated from your Go code.
- **Desktop APIs**: windows, menus, tray, dialogs, notifications, global
  shortcuts, deep links, file associations and more.
- **Ready to ship**: app bundles and disk images, Windows installers, Debian
  packages, code signing, notarization, and signed auto-updates with an
  update window in the manner of Sparkle.

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

## Getting started

With [Go](https://go.dev/dl/) 1.27+ and [Bun](https://bun.sh):

```sh
bunx mygo-cli init my-app      # or: npx mygo-cli init my-app
cd my-app
bun run dev
```

Or, without npm — the CLI is a Go program, which Go runs too:

```sh
go run github.com/egoist/mygo/cmd/mygo@latest init my-app
cd my-app
bun run dev
```

Read the [documentation](docs/README.md).

## Status

MyGo is at v0.1: the system webview on macOS 12+, Linux and Windows 10+
(x64 and arm64). v0.2 will add a bundled CEF option.

## License

MIT
