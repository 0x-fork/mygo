package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type buildOptions struct {
	debug        bool
	sign         string // macOS signing identity
	skipDMG      bool
	skipNotarize bool
	pkg          string            // directory of the main package
	work         string            // for generated files
	overlay      map[string]string // go build -overlay entries (the frontend)
}

func runBuild(args []string) error {
	flags := newFlags("build", "[flags] [dir]", `Builds a production app for each platform. It runs buildCommand from
mygo.json, then compiles the app with the frontendDist files embedded, served
at mygo://localhost/. macOS gets a signed .app bundle and a
"<name> <version>.dmg" disk image whose window invites dragging the app to
Applications; other platforms get an executable. The contents of the
resources directory and the resources listed in mygo.json are copied into
the bundle's Contents/Resources, or next to the executable. MyGo needs no
cgo, so any platform can be compiled from any machine; signing and disk
images need macOS.

Set macos.signingIdentity in mygo.json (or -sign) to a Developer ID to ship
outside the Mac App Store, and macos.notarize to notarize the disk image.`)
	platforms := flags.String("platform", runtime.GOOS+"/"+runtime.GOARCH, "comma separated GOOS/GOARCH targets, e.g. darwin/universal,linux/amd64,windows/amd64")
	debug := flags.Bool("debug", false, "keep development features such as the web inspector")
	skipBuildCommand := flags.Bool("skip-build-command", false, "do not run buildCommand")
	skipDMG := flags.Bool("skip-dmg", false, "do not create a disk image for macOS")
	skipNotarize := flags.Bool("skip-notarize", false, "do not notarize even when macos.notarize is set")
	sign := flags.String("sign", "", `macOS signing identity (default: macos.signingIdentity, or "-" for ad hoc)`)
	out := flags.String("o", "", "output directory (default: out from mygo.json or dist)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	c, err := loadConfig(dirArg(flags.Args()))
	if err != nil {
		return err
	}
	if *out != "" {
		c.Out = *out
	}
	opts := buildOptions{debug: *debug, sign: c.MacOS.SigningIdentity, skipDMG: *skipDMG, skipNotarize: *skipNotarize}
	if *sign != "" {
		opts.sign = *sign
	}
	if c.MacOS.Notarize != nil && !opts.skipNotarize && opts.sign == "-" {
		return fmt.Errorf("notarization needs a Developer ID signing identity: set macos.signingIdentity or pass -sign")
	}

	// The TypeScript client comes first: the frontend build type-checks and
	// bundles it.
	host := tempBinary(c.executableName())
	defer os.Remove(host)
	if err := buildBinary(c, host, nil); err != nil {
		return err
	}
	if err := generateBindings(c, host); err != nil {
		return err
	}
	if c.BuildCommand != "" && !*skipBuildCommand {
		if err := run(c.root, c.BuildCommand); err != nil {
			return err
		}
	}
	work, err := os.MkdirTemp("", "mygo-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	opts.work = work
	if opts.pkg, err = packageDir(c); err != nil {
		return err
	}
	// Left behind by an interrupted build.
	for _, p := range sysoFiles(opts.pkg) {
		if strings.HasPrefix(filepath.Base(p), "mygo_windows_") {
			os.Remove(p)
		}
	}
	if opts.overlay, err = frontendFiles(c, opts.pkg, work); err != nil {
		return err
	}

	for _, p := range splitList(*platforms) {
		goos, goarch, ok := strings.Cut(p, "/")
		if !ok {
			return fmt.Errorf("invalid platform %q, want GOOS/GOARCH", p)
		}
		paths, err := buildPlatform(c, goos, goarch, opts)
		if err != nil {
			return err
		}
		for _, path := range paths {
			rel, _ := filepath.Rel(c.root, path)
			logf("built %s (%s)", rel, sizeOf(path))
		}
	}
	return nil
}

// buildPlatform builds and packages the app for one platform into
// <out>/<goos>-<goarch> and returns the artifacts. Everything is prepared
// in a staging directory that then replaces the platform directory, so a
// failed build keeps the previous one and nothing stale remains.
func buildPlatform(c *Config, goos, goarch string, opts buildOptions) ([]string, error) {
	out := c.path(c.Out)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(out, ".staging-"+goos+"-"+goarch+"-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if err := os.Chmod(stage, 0o755); err != nil {
		return nil, err
	}
	res, err := c.resources(reservedNames(c, goos)...)
	if err != nil {
		return nil, err
	}

	ldflags := "-s -w" + packageFlags(c)
	if !opts.debug {
		ldflags += " -X github.com/egoist/mygo.production=1"
	}
	if goos == "windows" {
		// A GUI app, not a console one.
		ldflags += " -H=windowsgui"
	}
	compile := func(arch, out string) error {
		logf("building %s/%s", goos, arch)
		if goos == "windows" {
			cleanup, err := windowsResources(c, opts.pkg, arch)
			if err != nil {
				return err
			}
			defer cleanup()
		}
		overlay, err := writeOverlay(filepath.Join(opts.work, "overlay.json"), opts.overlay)
		if err != nil {
			return err
		}
		flags := []string{"-trimpath", "-ldflags", ldflags}
		if overlay != "" {
			flags = append(flags, "-overlay", overlay)
		}
		return buildBinary(c, out, []string{"GOOS=" + goos, "GOARCH=" + arch}, flags...)
	}

	name := c.executableName()
	var artifacts []string
	switch goos {
	case "darwin":
		bin := filepath.Join(stage, name)
		if goarch == "universal" {
			arm, amd := bin+".arm64", bin+".amd64"
			if err := compile("arm64", arm); err != nil {
				return nil, err
			}
			if err := compile("amd64", amd); err != nil {
				return nil, err
			}
			if err := writeUniversal(bin, arm, amd); err != nil {
				return nil, err
			}
			os.Remove(arm)
			os.Remove(amd)
		} else if err := compile(goarch, bin); err != nil {
			return nil, err
		}
		icns, err := appIcon(c)
		if err != nil {
			return nil, err
		}
		app, err := writeBundle(c, stage, bin, icns, res)
		if err != nil {
			return nil, err
		}
		if err := codesign(c, app, opts.sign, true); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, app)
		switch {
		case opts.skipDMG:
		case runtime.GOOS != "darwin":
			logf("skipping the disk image: it needs macOS")
		default:
			dmg, err := buildDMG(c, app, stage, opts)
			if err != nil {
				return nil, err
			}
			artifacts = append(artifacts, dmg)
		}
	case "windows":
		exe := filepath.Join(stage, name+".exe")
		if err := compile(goarch, exe); err != nil {
			return nil, err
		}
		if err := copyResources(res, stage); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, exe)
	default:
		exe := filepath.Join(stage, slugify(name))
		if err := compile(goarch, exe); err != nil {
			return nil, err
		}
		files, err := writeLinuxDesktop(c, stage, slugify(name))
		if err != nil {
			return nil, err
		}
		if err := copyResources(res, stage); err != nil {
			return nil, err
		}
		artifacts = append(append(artifacts, exe), files...)
	}

	final := filepath.Join(out, goos+"-"+goarch)
	if err := replacePath(stage, final); err != nil {
		return nil, err
	}
	for i, a := range artifacts {
		artifacts[i] = filepath.Join(final, filepath.Base(a))
	}
	if len(res) > 0 && goos != "darwin" {
		logf("copied %d resources next to the executable", len(res))
	}
	return artifacts, nil
}

// packageFlags are the -ldflags that link what mygo.json says about the app
// into it: Linux executables carry no metadata of their own, and the URL
// schemes tell every platform which launch arguments are deep links.
func packageFlags(c *Config) string {
	var b strings.Builder
	for _, v := range [][2]string{
		{"packageName", c.Name},
		{"packageVersion", c.Version},
		{"packageIdentifier", c.Identifier},
		{"packageURLSchemes", strings.Join(c.URLSchemes, ",")},
	} {
		if v[1] != "" {
			b.WriteString(" -X " + ldflagsQuote("github.com/egoist/mygo."+v[0]+"="+v[1]))
		}
	}
	return b.String()
}

// ldflagsQuote quotes an argument in -ldflags, which go build splits at
// spaces outside single or double quotes, without escapes (loadConfig
// rejects values with both kinds of quotes).
func ldflagsQuote(s string) string {
	switch {
	case !strings.ContainsAny(s, " \t'\""):
		return s
	case !strings.Contains(s, "'"):
		return "'" + s + "'"
	}
	return `"` + s + `"`
}

// windowsResources puts the resources of the executable (icon, manifest,
// version) in the main package for one build, unless the app has resources
// of its own. go build -overlay does not apply to .syso files, so the file
// is written into the package and removed by cleanup.
func windowsResources(c *Config, pkg, arch string) (cleanup func(), err error) {
	cleanup = func() {}
	for _, p := range sysoFiles(pkg) {
		if !strings.HasPrefix(filepath.Base(p), "mygo_windows_") {
			logf("using the .syso resources of the app")
			return cleanup, nil
		}
	}
	syso, err := winresSyso(c, arch)
	if err != nil {
		return cleanup, err
	}
	path := filepath.Join(pkg, "mygo_windows_"+arch+".syso")
	if err := os.WriteFile(path, syso, 0o644); err != nil {
		return cleanup, err
	}
	return func() { os.Remove(path) }, nil
}

func sysoFiles(pkg string) []string {
	files, _ := filepath.Glob(filepath.Join(pkg, "*.syso"))
	return files
}

func sizeOf(path string) string {
	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return fmt.Sprintf("%.1f MB", float64(total)/(1<<20))
}

// writeLinuxDesktop writes a .desktop entry, and the icon, for the binary
// name in dir and returns the files written.
func writeLinuxDesktop(c *Config, dir, name string) ([]string, error) {
	var files []string
	icon := ""
	if c.Icon != "" {
		data, err := os.ReadFile(c.path(c.Icon))
		if err != nil {
			return nil, err
		}
		icon = name
		path := filepath.Join(dir, icon+".png")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return nil, err
		}
		files = append(files, path)
	}
	entry := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=%s\nExec=%s\nIcon=%s\nCategories=Utility;\nTerminal=false\n", c.Name, name, icon)
	if len(c.URLSchemes) > 0 {
		// Opens deep links, which reach mygo.App.OnOpenURL.
		entry = strings.Replace(entry, "Exec="+name+"\n", "Exec="+name+" %u\n", 1)
		entry += "MimeType="
		for _, s := range c.URLSchemes {
			entry += "x-scheme-handler/" + s + ";"
		}
		entry += "\n"
	}
	path := filepath.Join(dir, name+".desktop")
	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		return nil, err
	}
	return append(files, path), nil
}
