package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Config is read from mygo.json in the project root. Every field is
// optional.
type Config struct {
	// Name is the display name of the app (default: directory name).
	Name string `json:"name"`
	// Identifier is the reverse-DNS bundle identifier.
	Identifier string `json:"identifier"`
	Version    string `json:"version"`
	Copyright  string `json:"copyright"`
	// Icon is a square PNG, ideally 1024x1024 (default:
	// resources/icon.png when it exists).
	Icon string `json:"icon"`
	// Main is the Go package of the app (default ".").
	Main string `json:"main"`
	// Out is where builds are written (default "dist").
	Out string `json:"out"`
	// Bindings is the path of the generated TypeScript client (default:
	// frontend/src/mygo.ts when there is a frontend directory,
	// src/mygo.ts when the frontend is the project itself (package.json),
	// else mygo.ts).
	Bindings string `json:"bindings"`

	// DevURL is what the app loads during `mygo dev` in place of its built
	// frontend, usually the dev server that DevCommand starts, e.g.
	// "http://localhost:5173". Windows load it with relative URLs such as
	// "/" (see mygo.WindowOptions.URL).
	DevURL string `json:"devUrl"`
	// DevCommand runs in the project directory while `mygo dev` runs, e.g.
	// "bun run --cwd frontend dev". mygo dev launches the app once DevURL
	// answers.
	DevCommand string `json:"devCommand"`
	// BuildCommand builds the frontend before `mygo build` compiles the
	// app, e.g. "bun run --cwd frontend build". It runs in the project
	// directory.
	BuildCommand string `json:"buildCommand"`
	// FrontendDist is the directory of the built frontend. `mygo build`
	// embeds it into the app, which serves it at mygo://localhost/. During
	// `mygo dev` without DevURL, it is served from disk.
	FrontendDist string `json:"frontendDist"`

	// Resources lists extra files and directories to ship with the app.
	// Each is copied under its base name into the app's resource directory
	// (mygo.PathResources), next to the contents of the project's resources
	// directory, which are always included.
	Resources []string `json:"resources"`

	// URLSchemes are the custom URL schemes (deep links) of the app, e.g.
	// "myapp" for myapp://…, whose URLs reach mygo.App.OnOpenURL. macOS
	// registers them with the app bundle, Linux with the desktop entry
	// `mygo build` writes; apps register them at run time with
	// mygo.App.RegisterURLScheme where there is no installer.
	URLSchemes []string `json:"urlSchemes"`
	// Updates configures signed updates, which mygo.Updater installs.
	Updates *Updates `json:"updates"`
	MacOS   MacOS    `json:"macos"`

	root string
}

// MacOS configures macOS packaging.
type MacOS struct {
	// MinimumSystemVersion is the oldest macOS version supported (default
	// "12.0").
	MinimumSystemVersion string `json:"minimumSystemVersion"`
	// SigningIdentity signs the app and the disk image, e.g.
	// "Developer ID Application: Jane Doe (TEAMID)". The default "-" signs
	// ad hoc: the app runs on the Mac that built it, but Gatekeeper blocks
	// it on others.
	SigningIdentity string `json:"signingIdentity"`
	// Entitlements is a plist of entitlements to sign the app with.
	Entitlements string `json:"entitlements"`
	// DMGTitle is the volume name of the disk image (default: name).
	DMGTitle string `json:"dmgTitle"`
	// Notarize submits production disk images to Apple's notary service and
	// staples the ticket. It needs a Developer ID signing identity.
	Notarize *Notarize `json:"notarize"`
}

// Notarize holds notarytool credentials stored in the keychain with
// `xcrun notarytool store-credentials <profile>`.
type Notarize struct {
	KeychainProfile string `json:"keychainProfile"`
	// Keychain is the keychain holding the profile (default: the login
	// keychain).
	Keychain string `json:"keychain"`
}

func loadConfig(root string) (*Config, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	c := &Config{root: abs}
	data, err := os.ReadFile(filepath.Join(abs, "mygo.json"))
	switch {
	case err == nil:
		if err := json.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("mygo.json: %w", err)
		}
	case !os.IsNotExist(err):
		return nil, err
	}
	c.applyDefaults()
	if n := c.MacOS.Notarize; n != nil && n.KeychainProfile == "" {
		return nil, fmt.Errorf("mygo.json: macos.notarize needs a keychainProfile (see xcrun notarytool store-credentials)")
	}
	if c.Updates != nil {
		if err := c.Updates.validate(); err != nil {
			return nil, err
		}
	}
	for _, scheme := range c.URLSchemes {
		if !schemeRe.MatchString(scheme) {
			return nil, fmt.Errorf("mygo.json: urlSchemes: %q is not a URL scheme (a letter, then letters, digits, +, - or .)", scheme)
		}
	}
	for _, v := range []string{c.Name, c.Version, c.Identifier} {
		if strings.Contains(v, "'") && strings.Contains(v, `"`) {
			return nil, fmt.Errorf("mygo.json: %q cannot contain both kinds of quotes", v)
		}
	}
	if c.DevURL != "" {
		if u, err := url.Parse(c.DevURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("mygo.json: devUrl %q is not an http(s) URL", c.DevURL)
		}
	}
	return c, nil
}

var (
	nonIdent = regexp.MustCompile(`[^a-zA-Z0-9]+`)
	schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*$`)
)

func (c *Config) applyDefaults() {
	if c.Name == "" {
		c.Name = filepath.Base(c.root)
	}
	if c.Identifier == "" {
		slug := strings.ToLower(nonIdent.ReplaceAllString(c.Name, ""))
		if slug == "" {
			slug = "app"
		}
		c.Identifier = "com.mygo." + slug
	}
	if c.Version == "" {
		c.Version = "0.1.0"
	}
	if c.Main == "" {
		c.Main = "."
	}
	if c.Out == "" {
		c.Out = "dist"
	}
	if c.Icon == "" && fileExists(filepath.Join(c.root, resourcesDir, "icon.png")) {
		c.Icon = filepath.Join(resourcesDir, "icon.png")
	}
	if c.MacOS.MinimumSystemVersion == "" {
		c.MacOS.MinimumSystemVersion = "12.0"
	}
	if c.MacOS.SigningIdentity == "" {
		c.MacOS.SigningIdentity = "-"
	}
	if c.MacOS.DMGTitle == "" {
		c.MacOS.DMGTitle = c.Name
	}
	if c.Bindings == "" {
		switch {
		case isDir(filepath.Join(c.root, "frontend")):
			c.Bindings = filepath.Join("frontend", "src", "mygo.ts")
		case fileExists(filepath.Join(c.root, "package.json")):
			c.Bindings = filepath.Join("src", "mygo.ts")
		default:
			c.Bindings = "mygo.ts"
		}
	}
}

func (c *Config) path(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.root, p)
}

// executableName is a file system friendly version of Name.
func (c *Config) executableName() string {
	if name := fsName(c.Name); name != "" {
		return name
	}
	return "app"
}

// fsName makes s usable as a file name on every platform.
func fsName(s string) string {
	return strings.Map(func(r rune) rune {
		if r < ' ' || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(s))
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
