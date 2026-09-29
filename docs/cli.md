# The mygo CLI

`mygo` creates, develops, packages and publishes MyGo apps.

## Install

Projects depend on the CLI as the `mygo-cli` npm package, which holds a
prebuilt binary for macOS, Linux and Windows on arm64 and x64, and run it
from their scripts:

```sh
bun add -d mygo-cli    # or: npm install -D mygo-cli
bun run dev            # "dev": "mygo dev" in package.json
bunx mygo doctor       # any command, in the project
```

Outside a project, run `bunx mygo-cli` or `npx mygo-cli`, e.g.
`bunx mygo-cli init my-app`: npm has an unrelated package named `mygo`, so
`bunx mygo` would run it where mygo-cli is not installed. `npm install -g
mygo-cli` puts `mygo` on your `PATH`. `MYGO_CLI_BINARY` makes the package
run another build of the CLI.

The CLI is a Go program, which Go runs too, without installing it:

```sh
go run github.com/egoist/mygo/cmd/mygo@latest init my-app
```

`go install` puts `mygo` on your `PATH`:

```sh
go install github.com/egoist/mygo/cmd/mygo@latest
```

Either way, building apps needs Go, which compiles them.

The commands that take a project directory, `[dir]`, default to the
current directory.

## mygo init

```
mygo init [flags] <dir>
```

Creates a project in a new or empty directory: a Go module and a
TypeScript frontend built with Vite, side by side, a default icon in
`resources/icon.png`, and a package.json with the scripts `dev`, `build`
and `generate`. It installs the dependencies with Bun and generates the
TypeScript client. See [the project](getting-started.md#the-project).

| Flag | |
|---|---|
| `-name` | the app's name (default: the directory's name) |
| `-module` | the Go module path (default: the directory's name) |
| `-mygo` | a checkout of MyGo to use, through a `replace` directive, instead of the released module; the scripts then run the checkout's CLI with `go run`; run `bun install && bun run build` in the checkout first |

## mygo dev

```
mygo dev [flags] [dir]
```

Develops the app with live reload. It writes the TypeScript client, runs
`devCommand` from the configuration, such as a Vite dev server, waits for
`devUrl` to answer, then builds a development app, which loads `devUrl` in
place of its built frontend, and starts it. Without `devUrl` the app serves
`frontendDist` from disk.

Changes to the Go code, the configuration, the icon or the resources rebuild the
app, regenerate the TypeScript client and restart the app. The new build
replaces the running one once it has started, so a build that fails or
crashes keeps the previous one. Frontend changes are the dev server's to
handle. Quitting the app, or Ctrl+C, ends mygo dev, and `App.Relaunch`
restarts the app.

The development app is named `<name> Dev`, with the identifier
`<identifier>.dev`, so that its data and preferences stay apart from the
installed app's. On macOS it is a real app bundle, in `.mygo/dev`.

| Flag | |
|---|---|
| `-skip-dev-command` | does not run `devCommand`, e.g. when the dev server already runs |
| `-sign` | the identity that signs the development app on macOS (default: `-`, ad hoc) |

## mygo build

```
mygo build [flags] [dir]
```

Builds production apps: runs `buildCommand`, compiles the app with
`frontendDist` embedded, and packages it for each platform in
`<out>/<os>-<arch>`. See [Building and distributing](distribution.md).

| Flag | |
|---|---|
| `-platform` | comma separated `GOOS/GOARCH` targets, e.g. `darwin/universal,windows/amd64,linux/amd64` (default: this machine's) |
| `-debug` | keeps development features, such as the web inspector |
| `-o` | the output directory (default: `out` of the configuration) |
| `-sign` | the macOS signing identity (default: `macos.signingIdentity`) |
| `-skip-build-command` | does not run `buildCommand` |
| `-skip-dmg` | does not make macOS disk images |
| `-skip-notarize` | does not notarize, even with `macos.notarize` set |
| `-upload` | uploads the installers and updates to a draft GitHub release of the version (`updates.github`), with `gh` |

## mygo generate

```
mygo generate [-o file] [dir]
```

Writes the typed TypeScript client for the services bound with `mygo.Bind`
and the events declared with `mygo.NewEvent`, to `bindings` of the
configuration unless `-o` names another file. `mygo dev` and `mygo build` run
it for you. See [the generated client](bindings.md#the-generated-client).

## mygo keygen

```
mygo keygen [flags]
```

Creates the key pair that signs [updates](updates.md): `mygo-update.key`,
the secret, and `mygo-update.pub`, and prints the `updates` section to add
to the configuration.

| Flag | |
|---|---|
| `-o` | the directory to write the keys to (default: `mygo/update-keys` in the user's configuration directory) |
| `-force` | replaces existing keys |

## mygo doctor

Checks the development machine: Go, Bun, the webview of the platform, and
the tools of optional features: NSIS for Windows installers, `signtool` or
`osslsigncode` for signing Windows apps, `gh` for `-upload`, and Developer
ID identities for signing macOS apps.

## mygo version

Prints the version of the CLI.

## Environment variables

| Variable | |
|---|---|
| `MYGO_UPDATER_PRIVATE_KEY` | the secret key that signs updates, for `mygo build` |
| `MYGO_WINDOWS_CERTIFICATE_PASSWORD` | the password of `windows.certificate`, for `mygo build` |
| `MYGO_CLI_BINARY` | a build of the CLI for the `mygo-cli` package to run |
| `MYGO_ENV` | `production` makes an app behave like a production build, e.g. without the web inspector |
