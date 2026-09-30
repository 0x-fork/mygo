# Auto-updates

Apps built with MyGo update themselves from signed releases, published on
GitHub or on any HTTPS server. `mygo build` signs each platform's build
with your key, and `mygo.Updater` in the app downloads a newer version,
checks the signature against the public key built into the app, and
replaces the app with it.

## Set up

Create the key pair that signs updates:

```sh
mygo keygen
```

It writes `mygo-update.key`, the secret key, and `mygo-update.pub`, the
public key, to `mygo/update-keys` in your configuration directory, and
prints where (`-o` chooses another directory). Keep the secret key out of the repository, in a password
manager and in the secrets of your CI: installed apps accept only updates
signed with it, so losing it strands them, and anyone who has it can ship
code to your users.

Add the public key to mygo.config.ts, with where releases are published:

```ts
export default defineConfig({
  version: "1.2.0",
  updates: {
    publicKey: "…the contents of mygo-update.pub…",
    github: "you/my-app",
  },
});
```

`github` is a public repository whose releases, tagged `v1.2.0` and so on
(`tagPrefix` changes the `v`), hold the updates. For your own server, set
`url` instead, an HTTPS URL of a directory:

```ts
export default defineConfig({
  updates: {
    publicKey: "…",
    url: "https://downloads.example.com/my-app",
  },
});
```

## Build and publish

Give `mygo build` the secret key, in the `MYGO_UPDATER_PRIVATE_KEY`
environment variable or as a file with `updates.privateKey` in mygo.config.ts
(a path, which may start with `~/`):

```sh
MYGO_UPDATER_PRIVATE_KEY="$(cat path/to/mygo-update.key)" bun run build
```

Next to the installers of each platform it writes:

- `my-app-1.2.0-darwin-arm64.tar.gz`, the app as installed,
- [delta updates](#delta-updates) from the versions before, such as
  `my-app-1.1.0-to-1.2.0-darwin-arm64.delta`, and
- `update-darwin-arm64.json`, the manifest: the version, its release
  notes, the date, and the URL, size and signature of the archive and of
  each delta.

Without a key, `mygo build` builds the apps and skips the update files.

The release notes are the section of the version in `CHANGELOG.md`
(`updates.changelog` names another file), under a `## 1.2.0` heading. When
the file exists, it must have that section.

Publish the files where `updates` points to:

- **GitHub**: `mygo build -upload` uploads them, with the installers, to a
  draft release of the version. Publishing the release makes it the latest,
  which apps check: they read the manifests from
  `https://github.com/you/my-app/releases/latest/download/`.
- **Your server**: upload the archives, deltas and manifests to the `url`
  directory, and keep the files of earlier versions there. Upload the
  manifests last, so apps never see a manifest whose files are missing.

Each platform, such as `darwin-arm64`, `darwin-universal` or
`windows-amd64`, has its own manifest, and a build only looks at its own.

## Delta updates

As with Sparkle, apps download only what changed since their version,
usually a small part of the whole app: a new version of a Go executable
mostly moves code around, which a binary patch describes in little space.

When `mygo build` signs an update, it reads the manifest of each platform
that is published, the one apps check, and downloads the archives of the
last 3 versions: the published one and the earlier ones its manifest lists
(`updates.deltas` changes how many, `0` makes none). It checks that they are
signed with your key and writes a delta from each to the new version:
files that did not change are copied from the installed app, changed ones
are patched ([bsdiff](https://www.daemonology.net/bsdiff/)), and the rest
are included. The manifest lists the deltas, and the archives of the
versions before for the next build. A delta that is not smaller than the
archive is left out. Builds need to download the published updates then;
when they cannot, such as for the first version, they make no deltas and
say why.

An app whose version has a delta downloads it, checks its signature and
makes the new version from itself next to it, checking that every file has
the size and SHA-256 that the delta gives, so that the result is exactly
the version published, code signature included. When anything fails, for
example because the app was changed since it was installed, it downloads
the archive instead.

## The update window

The updater plugin gives an app the update window that Mac users know from
Sparkle, on every platform:

```go
import "github.com/egoist/mygo/plugins/updater"

mygo.Use(updater.Plugin)
```

- It checks in the background once a day, the first time 10 seconds after
  launch when a check is due. When a new version is out, a window shows its
  release notes and offers **Install Update**, **Remind Me Later** and
  **Skip This Version**. Background checks do not offer a skipped version
  again, but offer the next one.
- Installing downloads the update with a progress bar, then offers to
  relaunch; otherwise the update runs at the next launch.
- With the window's "Automatically download and install updates in the
  future" checked, background checks install updates without asking, and
  they run at the next launch.
- `updater.CheckForUpdates()` checks as the user asked: the window shows at
  once, and says when the app is up to date or the check failed.
  `updater.MenuItem()` is a "Check for Updates…" item that calls it, which
  goes after About on macOS:

```go
mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
	{Label: "My App", Submenu: []*mygo.MenuItem{
		{Role: mygo.RoleAbout},
		updater.MenuItem(),
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	}},
	{Role: mygo.RoleEditMenu},
	{Role: mygo.RoleWindowMenu},
}))
```

`updater.New` takes options instead of the defaults of `updater.Plugin`:

```go
mygo.Use(updater.New(updater.Options{
	Interval:               12 * time.Hour, // between checks (default a day)
	DisableAutomaticChecks: true,           // until SetAutomaticChecks(true)
	Icon:                   iconPNG,        // default: icon.png among the resources
}))
```

The user's choices are kept in `updater.json` in `PathUserData`.
`AutomaticChecks` and `AutomaticDownloads` read them and
`SetAutomaticChecks` and `SetAutomaticDownloads` change them, for a
preferences page; `LastCheck` returns when the app last checked. Sparkle
asks on the second launch whether to check automatically: apps that want to
ask set `DisableAutomaticChecks` and call `SetAutomaticChecks` with the
answer.

Builds that cannot update themselves, such as development builds and apps
installed by a package manager, never check in the background, and the
window says why when the user checks.

### Languages

The window speaks the user's language (`App.Locale`) when the plugin has
it: English, Chinese (Simplified and Traditional), Dutch, French, German,
Italian, Japanese, Korean, Polish, Portuguese (Brazil), Russian, Spanish,
Turkish and Ukrainian, with their decimal marks and units ("1,5 Mo").
Other languages get English. `Language` picks one, such as the language the
app itself shows, and `Strings` changes texts or adds languages, by language
tag; the fields an app leaves empty keep the plugin's texts:

```go
mygo.Use(updater.New(updater.Options{
	Language: settings.Language, // "" follows the system
	Strings: map[string]updater.Strings{
		"en": {Install: "Update Now"},
		"sv": {Title: "Programuppdatering", Install: "Installera uppdatering" /* … */},
	},
}))
```

Some texts are formats, such as `AvailableMessage`, whose arguments are the
app's name, the new version and the running one: indexed verbs (`%[2]s`)
let a language order them. `mygo.Use` fails when a format does not fit its
arguments. Languages written from right to left, such as Arabic and Hebrew,
get a mirrored window, and release notes take the direction of the language
they are written in.

## Your own update UI

`mygo.Updater` is what the plugin is built on. Use it for an interface of
your own, such as a banner in the app's page:

```go
// UpdateProgress tells pages how far the download got.
var UpdateProgress = mygo.NewEvent[float64]("update-progress")

func checkForUpdates(ctx context.Context) {
	if !mygo.Updater.Enabled() {
		return
	}
	up, err := mygo.Updater.Check(ctx)
	if err != nil || up == nil {
		return // offline, or up to date
	}
	res, _ := mygo.Dialog.Message(mygo.MessageOptions{
		Message: "Version " + up.Version + " is available.",
		Detail:  up.Notes, // Markdown
		Buttons: []string{"Install and Restart", "Later"},
	})
	if res.Button != 0 {
		return
	}
	err = up.Install(ctx, func(downloaded, total int64) {
		UpdateProgress.Broadcast(float64(downloaded) / float64(total))
	})
	if err == nil {
		mygo.App.Relaunch()
	}
}
```

- `Check` returns the newer version, with its `Version`, `Notes` and
  `Date`, or nil when the app is up to date.
- `Install` downloads the archive, or the delta of the running version,
  checks its signature, and replaces the app. The progress callback gets
  the bytes of the delta, and when the delta fails, starts over with those
  of the archive. The running app is not affected: the new version runs
  after `App.Relaunch`, or at the next launch.
- `Enabled` reports whether the app can update itself: it was built with
  updates, and it can write where it is installed. Development builds
  cannot, and `Check` returns `mygo.ErrUpdatesDisabled` in them.

## Where apps can update

An app replaces itself, so it must be able to write where it is installed:

- macOS: the app bundle, e.g. in `/Applications` for an administrator, or
  in the user's `~/Applications`.
- Windows: the per-user install of the [installer](distribution.md#the-installer),
  in `%LOCALAPPDATA%\Programs`.
- Linux: a directory of the user, such as the extracted build.

Apps installed by a package manager, such as the Debian package in
`/opt`, are updated by it instead: `Enabled` is false for them.
