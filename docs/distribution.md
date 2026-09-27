# Building and distributing

`mygo build` turns a project into apps people install: an app bundle and a
disk image on macOS, an executable and an installer on Windows, an
executable, a desktop entry and a Debian package on Linux.

```sh
bun run build     # mygo build
```

It:

1. writes the TypeScript client, so the frontend builds against the Go
   code;
2. runs `buildCommand` from mygo.json, which builds the frontend into
   `frontendDist`;
3. compiles the app with the frontend embedded, for production: without
   the web inspector, and with the name, identifier and version of
   mygo.json linked in;
4. packages it for each platform in `build/<os>-<arch>/` (`out` in
   mygo.json);
5. signs what it can, and with [updates](updates.md) configured, writes the
   signed update archives.

## Platforms

`-platform` lists `GOOS/GOARCH` targets, by default the machine's own:

```sh
mygo build -platform darwin/universal,windows/amd64,windows/arm64,linux/amd64,linux/arm64
```

`darwin/universal` combines arm64 and amd64 in one app. MyGo needs no cgo,
so any machine compiles for every platform. Some steps need the tools of a
platform, and are skipped with a note elsewhere: macOS apps are signed and
put in disk images on macOS only.

| Platform | In `build/<os>-<arch>/` |
|---|---|
| macOS | `My App.app`, signed, and `My App 0.1.0.dmg` |
| Windows | `My App.exe`, the files of the app, and `My App Setup 0.1.0.exe` when [NSIS](https://nsis.sourceforge.io) (`makensis`) is installed |
| Linux | `my-app`, `my-app.desktop`, `my-app.png`, the files of the app, and `my-app_0.1.0_amd64.deb` when `linux.maintainer` is set |

Other flags: `-debug` keeps development features such as the inspector,
`-skip-dmg` and `-skip-notarize` skip those steps, `-sign` overrides the
signing identity of macOS, and `-o` the output directory. See
[the CLI](cli.md#mygo-build).

## Name, icon and version

mygo.json describes the app:

```json
{
  "name": "My App",
  "identifier": "com.example.myapp",
  "version": "1.2.0",
  "copyright": "© 2026 Example Inc."
}
```

- `name` is what users see: the app bundle, the executable, menus, the
  installer.
- `identifier` is a reverse DNS name unique to the app. Systems key
  preferences, permissions and registrations on it: keep it once released.
- `version` is the app's version, which `App.Version()` returns and
  [updates](updates.md) compare.

The icon is `resources/icon.png`, or the `icon` of mygo.json: a square PNG,
ideally 1024×1024. `mygo build` makes it the `.icns` of macOS, the icon
resource of the Windows executable and the icon of the Linux desktop entry.

## Resources

Files the app reads at run time, such as a database seed, a helper binary
or the images of a tray icon, ship with it. Everything in the `resources`
directory of the project is copied into the app, as are the files and
directories listed in `resources` in mygo.json, under their base names:

```json
{
  "resources": ["third_party/licenses", "bin/helper"]
}
```

At run time `App.Path(mygo.PathResources)` is where they are:
`Contents/Resources` in a macOS app, the executable's directory elsewhere,
and the project's `resources` directory under `go run` and `go test`:

```go
dir, _ := mygo.App.Path(mygo.PathResources)
seed := filepath.Join(dir, "seed.db")
```

`mygo dev` copies them into the development app too, and rebuilds it when
they change. Hidden files are left out. Executables among them are signed
with the app on macOS.

## macOS

`mygo build` makes an app bundle, signs it, and puts it in a disk image
whose window invites users to drag the app to Applications.

### Signing and notarization

Apps signed ad hoc, the default, run on the Mac that built them, but
Gatekeeper blocks them on others. To ship an app, sign it with a Developer
ID of the [Apple Developer Program](https://developer.apple.com/programs/)
and have Apple notarize it:

```json
{
  "macos": {
    "signingIdentity": "Developer ID Application: Jane Doe (TEAMID)",
    "notarize": { "keychainProfile": "notary" }
  }
}
```

Store the credentials of the notary service in the keychain once:

```sh
xcrun notarytool store-credentials notary
```

`mygo build` then signs the app with the hardened runtime, submits the disk
image to the notary service, and staples the ticket to the disk image and
to the app, which updates ship. `security
find-identity -v -p codesigning` lists your identities, and `mygo doctor`
counts the Developer IDs.

### Info.plist and entitlements

`macos.infoPlist` adds keys to the app's `Info.plist`, or replaces MyGo's,
for example the usage descriptions that pages using the camera or the
microphone need:

```json
{
  "macos": {
    "minimumSystemVersion": "13.0",
    "infoPlist": {
      "NSCameraUsageDescription": "Scan documents with the camera.",
      "LSApplicationCategoryType": "public.app-category.productivity"
    },
    "entitlements": "entitlements.plist",
    "dmgTitle": "My App Installer"
  }
}
```

`entitlements` signs the app with a property list of entitlements.
`minimumSystemVersion` is the oldest macOS the app runs on, 12.0 by
default.

## Windows

The executable carries the icon, the version information and a manifest
(per-monitor DPI awareness, modern controls), and runs without a console
window. A `.syso` file of your own in the main package replaces them.

### The installer

With NSIS installed (`brew install makensis`, `apt install nsis`, or
`choco install nsis` on Windows), `mygo build` also makes
`My App Setup 1.2.0.exe`. It installs the app for the current user in
`%LOCALAPPDATA%\Programs\My App`, which needs no administrator rights and
lets the app [update itself](updates.md), adds a Start menu shortcut and an
uninstaller listed in Settings, and registers the app's URL schemes and
file associations.

### Code signing

Unsigned apps make SmartScreen warn users. Sign the executable and the
installer with a certificate:

```json
{
  "windows": {
    "certificate": "certs/code-signing.pfx"
  }
}
```

The password comes from the `MYGO_WINDOWS_CERTIFICATE_PASSWORD`
environment variable. On Windows `signtool` signs, from the Windows SDK;
elsewhere `osslsigncode`. `timestampUrl` changes the time stamping server,
by default `http://timestamp.digicert.com`.

Certificates on hardware tokens or in cloud services, such as Azure Trusted
Signing, sign with a command of their own, in which `%1` is the file:

```json
{
  "windows": {
    "signCommand": "signtool sign /fd sha256 /tr http://timestamp.digicert.com /td sha256 /a %1"
  }
}
```

## Linux

`mygo build` writes the executable, named after the app in lower case, a
desktop entry and the icon. With a maintainer, it also makes a Debian
package, which installs the app in `/opt/my-app` with a `my-app` command,
its desktop entry, icons, URL schemes and file types:

```json
{
  "linux": {
    "maintainer": "Jane Doe <jane@example.com>",
    "comment": "Take notes",
    "categories": ["Office"],
    "depends": ["libayatana-appindicator3-1"]
  }
}
```

The package depends on GTK 3 and WebKitGTK; `depends` adds more packages.
`comment` describes the app in its desktop entry and package, and
`categories` places it in application menus (`Utility` by default).

## URL schemes and file types

`urlSchemes` and `fileAssociations` in mygo.json (see
[deep links](app.md#deep-links) and [file associations](app.md#file-associations))
are registered by each package: in the macOS app's `Info.plist`, by the
Windows installer, and by the Linux desktop entry and Debian package.

## Publishing

With `updates.github` set in mygo.json, `mygo build -upload` uploads the
disk images, installers, packages and update files to the GitHub release of
the version, tagged `v1.2.0`, creating it as a draft. Review the draft and
publish it. It needs the [GitHub CLI](https://cli.github.com) (`gh`),
signed in.

Build on each platform in CI and upload to the same release: macOS runners
sign, notarize and make the disk images; any runner compiles and packages
Windows and Linux apps. Keep the signing secrets in CI secrets:
`MYGO_WINDOWS_CERTIFICATE_PASSWORD`, `MYGO_UPDATER_PRIVATE_KEY`, and the
notary credentials in a keychain of the macOS runner.
