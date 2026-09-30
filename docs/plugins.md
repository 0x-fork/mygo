# Plugins

A plugin adds a feature in two halves: Go services, and a JavaScript
package whose functions call them. The app uses the Go half with
`mygo.Use` and imports the JavaScript half from npm.

## Official plugins

| Plugin | Go | npm | Gives pages |
|---|---|---|---|
| fetch | `github.com/egoist/mygo/plugins/fetch` | `@mygo-plugins/fetch` | a `fetch` that makes HTTP requests from Go: no CORS, any header, streamed bodies, cancellation with `AbortSignal` |
| websocket | `github.com/egoist/mygo/plugins/websocket` | `@mygo-plugins/websocket` | a `WebSocket` whose connections Go makes, with headers on the handshake |

The updater plugin, `github.com/egoist/mygo/plugins/updater`, is all Go:
it gives the app an update window in the manner of Sparkle, which checks
for updates in the background and offers to install them. See
[the update window](updates.md#the-update-window).

```go
import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/fetch"
	"github.com/egoist/mygo/plugins/websocket"
)

func main() {
	mygo.Use(fetch.Plugin, websocket.Plugin)
	// ...
}
```

```sh
bun add @mygo-plugins/fetch @mygo-plugins/websocket
```

```ts
import { fetch } from "@mygo-plugins/fetch";
import { WebSocket } from "@mygo-plugins/websocket";

const res = await fetch("https://api.example.com/me", {
  headers: { authorization: `Bearer ${token}` },
});
const ws = new WebSocket("wss://api.example.com/live", [], {
  headers: { authorization: `Bearer ${token}` },
});
```

`fetch.Plugin` and `websocket.Plugin` use the default options; `fetch.New`
and `websocket.New` take an `http.Client` (for proxies, cookies, TLS
settings) and an `Allow` function that decides which requests pages may
make. The packages' READMEs, in the repository's `plugins` directory, list
the details, and `go doc` documents the options.

Using a plugin's JavaScript package without its Go half fails with an error
that says to call `mygo.Use`.

## Writing a plugin

The Go half is a `mygo.Plugin`: a name, a service whose methods pages call,
and an optional `Setup` that runs when the app uses it:

```go
package clock

// Plugin gives pages the time of the Go side.
var Plugin = mygo.Plugin{Name: "clock", Service: service{}}

type service struct{}

func (service) Now() time.Time { return time.Now() }
```

The service is bound like those of `mygo.Bind`, with the same rules for its
methods (contexts, channels, errors), but as `plugin:<name>` and outside the
generated TypeScript client. Its JavaScript package calls it with `call` of
`mygo-runtime`, which it lists as a peer dependency, so that it uses the
app's copy (and as a development dependency, for its own builds and tests):

```ts
import { call } from "mygo-runtime";

export const now = async () => new Date(await call<string>("plugin:clock.Now"));
```

Only the app's trusted pages may call a plugin, as with every bound method.
`mygo.Use` panics when a name is taken or the service's methods use types
that cannot cross to JavaScript, so mistakes show at startup.
