# AGENTS.md

MyGo is a desktop application framework in Go built on the system webview
(WKWebView on macOS, WebKitGTK on Linux), with typed Go ↔ TypeScript IPC.

**Read [docs/architecture.md](docs/architecture.md) before changing code.** It
describes the layers, the threading model, native interop without cgo, the IPC
wire protocol, TypeScript generation, and the checklist for adding features.

## Hard rules

- **No cgo.** Everything must build with `CGO_ENABLED=0`. Call native code
  through purego (`internal/darwin`, `internal/linux`) or `syscall`
  (`internal/windows`), never `import "C"`.
- **Bun is dev tooling only.** The repository is a Bun workspace
  (`packages/bridge`, `packages/runtime`, and examples with a frontend, which
  keep it at their root); Bun also installs and runs the frontend template
  (Vite). Nothing in an app may need Bun at run time.
- **Scope:** system webview on macOS, Linux and Windows (WebView2). The
  bundled CEF option is out of scope until asked. Other platforms must keep
  compiling through `internal/unsupported`.
- **Great DX over Electron parity.** Keep the familiar feel (app lifecycle,
  windows, menus) but design Go-first APIs: typed IPC via `mygo.Bind` and
  `mygo.NewEvent[T]` with the generated client, not string channels.
- **Threading.** Backend methods and handler callbacks run on the main thread.
  Public methods must be safe from any goroutine (`onMain`/`postMain`/`await`
  in `loop.go`). Never block the main thread waiting on work that needs it.
- **purego callbacks are a scarce, never-freed resource.** Create them once
  per signature at startup and route by user data, never per window or call.

## Commands

```sh
go build ./... && GOOS=linux go build ./... && GOOS=windows go build ./...  # all must pass
go vet ./... && GOOS=linux go vet ./... && GOOS=windows go vet ./...
go test ./...                              # core (fake backend), tsgen, CLI, accelerator
MYGO_E2E=1 go test ./internal/e2e          # real GUI tests (macOS desktop session)
bun install && bun run test && bun run typecheck   # the workspace, from the root
bun run build                              # rebuild internal/bridge/bridge.js and packages/runtime/dist
go run ./cmd/mygo generate examples/todo   # regenerate an example's TypeScript client
go run ./cmd/mygo dev examples/todo        # live reload (dev bundle in examples/todo/.mygo)
go run ./cmd/mygo build examples/todo      # .app + .dmg in examples/todo/dist
```

Linux GUI tests cross-compile and run in a container with WebKitGTK and Xvfb;
Windows GUI tests need Windows with the WebView2 Runtime (a GitHub Actions
`windows-latest` runner has it). See "Testing" in the architecture guide.

## Conventions

- New behavior goes into `package mygo` first; backends only translate
  `internal/platform` calls to native ones. Implement every platform method in
  `darwin`, `linux`, `windows`, `fake` and `unsupported`.
- Objective-C: `alloc`/`init` objects are owned and must be released; wrap
  temporary objects in `withPool`; copy blocks you call later.
- Commit `internal/bridge/bridge.js` and `packages/runtime/dist` whenever
  their sources change, and keep the generated clients of examples
  (`examples/*/src/mygo.ts`) up to date. Built frontends are not
  committed: `mygo build` embeds them.
- `mygo-runtime` (`packages/runtime`) is released to npm with the same
  version as the Go module (`mygo.Version`); the template depends on it.
- Tests: unit tests through `internal/fake`; GUI behavior in `internal/e2e`.
