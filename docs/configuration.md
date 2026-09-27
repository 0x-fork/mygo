# mygo.json

`mygo.json`, at the root of a project, describes the app and how the CLI
develops and builds it. Every field is optional.

```json
{
  "name": "My App",
  "identifier": "com.example.myapp",
  "version": "1.2.0",
  "copyright": "© 2026 Example Inc.",

  "devUrl": "http://localhost:5173",
  "devCommand": "bun run dev:web",
  "buildCommand": "bun run build:web",
  "frontendDist": "dist",
  "bindings": "src/mygo.ts",
  "out": "build",

  "resources": ["third_party/licenses"],
  "urlSchemes": ["myapp"],
  "fileAssociations": [{ "ext": ["md"], "name": "Markdown Document", "mimeType": "text/markdown" }],

  "updates": { "publicKey": "…", "github": "you/my-app" },
  "macos": { "signingIdentity": "Developer ID Application: Jane Doe (TEAMID)" },
  "windows": { "certificate": "certs/code-signing.pfx" },
  "linux": { "maintainer": "Jane Doe <jane@example.com>" }
}
```

## The app

| Field | Default | |
|---|---|---|
| `name` | the directory's name | the name users see: the app, its executable, menus, installers |
| `identifier` | `com.mygo.` and the name's letters and digits, e.g. `com.mygo.myapp` | a reverse DNS name unique to the app, e.g. `com.example.myapp`; systems key preferences, permissions and registrations on it |
| `version` | `0.1.0` | the version of the app, which `App.Version()` returns and updates compare |
| `copyright` | | the copyright notice of the app, in its `Info.plist` (macOS) and file properties (Windows) |
| `icon` | `resources/icon.png` when it exists | a square PNG, ideally 1024×1024 |
| `main` | `.` | the Go package of the app, relative to the project |

## Development and the frontend

| Field | Default | |
|---|---|---|
| `devUrl` | | the dev server that `mygo dev` points the app at, an `http(s)` URL; windows load it for URLs without a scheme, such as `/` |
| `devCommand` | | runs in the project directory while `mygo dev` runs, e.g. a Vite dev server; the app starts once `devUrl` answers |
| `buildCommand` | | builds the frontend before `mygo build` compiles the app |
| `frontendDist` | | the directory of the built frontend, which `mygo build` embeds into the app; `mygo dev` serves it from disk when there is no `devUrl` |
| `bindings` | `frontend/src/mygo.ts` when there is a `frontend` directory, else `src/mygo.ts` when package.json is at the root, else `mygo.ts` | where `mygo generate` writes the TypeScript client |
| `out` | `dist` | where `mygo build` writes the builds, one directory per platform |

See [the frontend](frontend.md#how-pages-load) for how they fit together.
New projects set `out` to `build`, apart from Vite's `dist`.

## Integration

| Field | |
|---|---|
| `resources` | files and directories copied into the app, next to the contents of the `resources` directory, under their base names; see [resources](distribution.md#resources) |
| `urlSchemes` | the URL schemes the app opens, such as `myapp` for `myapp://…`, which reach `App.OnOpenURL`; see [deep links](app.md#deep-links) |
| `fileAssociations` | the file types the app opens, which reach `App.OnOpenFile`; see [file associations](app.md#file-associations) |

Each file association has:

| Field | |
|---|---|
| `ext` | the file name extensions, without dots: `["md", "markdown"]` |
| `name` | what the files are: `"Markdown Document"` |
| `mimeType` | their MIME type, by which Linux identifies files; without one the app defines `application/x-<app>-<ext>` |
| `role` | `Editor` (the default) or `Viewer`, on macOS |

## updates

Signed [auto-updates](updates.md). `publicKey` and one of `github` and
`url` are required.

| Field | Default | |
|---|---|---|
| `publicKey` | | the contents of `mygo-update.pub`, from `mygo keygen` |
| `github` | | a public repository, `owner/name`, whose releases hold the updates |
| `tagPrefix` | `v` | what precedes the version in release tags |
| `url` | | instead of `github`, the HTTPS URL of a directory holding the updates |
| `privateKey` | | the path of `mygo-update.key`, for `mygo build`; the `MYGO_UPDATER_PRIVATE_KEY` environment variable, holding the key, takes precedence |
| `changelog` | `CHANGELOG.md` when it exists | a Markdown file whose `## <version>` section becomes the release notes |

## macos

| Field | Default | |
|---|---|---|
| `minimumSystemVersion` | `12.0` | the oldest macOS the app runs on |
| `signingIdentity` | `-`, ad hoc | the identity that signs the app and its disk image, e.g. `Developer ID Application: Jane Doe (TEAMID)` |
| `entitlements` | | a property list of entitlements to sign the app with |
| `infoPlist` | | keys added to the app's `Info.plist`, replacing MyGo's, e.g. `NSCameraUsageDescription` |
| `dmgTitle` | `name` | the volume name of the disk image |
| `notarize` | | notarizes production disk images: `keychainProfile`, the profile of `xcrun notarytool store-credentials`, and optionally `keychain`, the keychain holding it |

See [macOS](distribution.md#macos).

## windows

| Field | Default | |
|---|---|---|
| `certificate` | | a code signing certificate (`.pfx`) for the executable and the installer; its password comes from `MYGO_WINDOWS_CERTIFICATE_PASSWORD` |
| `signCommand` | | instead of `certificate`, a command that signs a file, with `%1` for its path |
| `timestampUrl` | `http://timestamp.digicert.com` | the time stamping server of `certificate` |

See [Windows](distribution.md#windows).

## linux

| Field | Default | |
|---|---|---|
| `maintainer` | | `Name <email>`; makes `mygo build` create a Debian package |
| `comment` | | a short description, for the desktop entry and the package |
| `categories` | `["Utility"]` | the categories of the desktop entry, which place it in application menus |
| `depends` | | Debian packages the app needs besides GTK and WebKitGTK |

See [Linux](distribution.md#linux).
