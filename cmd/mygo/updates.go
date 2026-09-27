package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/internal/update"
)

// Signed updates (mygo.Updater): `mygo keygen` creates the key pair,
// `mygo build` signs an archive of each platform's app with the private key
// and writes update-<target>.json, which apps built with the public key
// check.

// Updates configures signed updates.
type Updates struct {
	// PublicKey is the contents of mygo-update.pub (mygo keygen). Installed
	// apps only accept updates signed with its private key.
	PublicKey string `json:"publicKey"`
	// GitHub is the public repository ("owner/name") whose releases hold
	// the updates: tagged TagPrefix+version, the latest release serving
	// the manifests.
	GitHub    string `json:"github"`
	TagPrefix string `json:"tagPrefix"`
	// URL is where the update files are served from instead, e.g.
	// "https://downloads.example.com/my-app".
	URL string `json:"url"`
	// PrivateKey is the path of mygo-update.key. MYGO_UPDATER_PRIVATE_KEY,
	// holding the key itself, takes precedence.
	PrivateKey string `json:"privateKey"`
	// Changelog is a Markdown file whose "## <version>" section becomes the
	// release notes (default CHANGELOG.md when it exists).
	Changelog string `json:"changelog"`
}

func (u *Updates) validate() error {
	if u.PublicKey == "" {
		return errors.New("updates needs the publicKey of mygo keygen")
	}
	if _, err := update.ParsePublicKey(u.PublicKey); err != nil {
		return fmt.Errorf("updates.publicKey: %w", err)
	}
	switch {
	case (u.GitHub == "") == (u.URL == ""):
		return errors.New("updates needs either github (owner/name) or url")
	case u.GitHub != "" && strings.Count(u.GitHub, "/") != 1:
		return fmt.Errorf("updates.github %q is not owner/name", u.GitHub)
	case u.URL != "":
		p, err := url.Parse(u.URL)
		if err != nil || p.Scheme != "https" || p.Host == "" {
			return fmt.Errorf("updates.url %q is not an https URL", u.URL)
		}
	}
	if u.TagPrefix == "" {
		u.TagPrefix = "v"
	}
	return nil
}

// updateFeed returns the URL of the manifest of target.
func (c *Config) updateFeed(target string) string {
	return c.updateFile(update.ManifestName(target), true)
}

// updateFile returns the URL of a published update file; manifests come
// from the latest release on GitHub, archives from the release of this
// version.
func (c *Config) updateFile(name string, latest bool) string {
	u := c.Updates
	if u.GitHub == "" {
		return strings.TrimSuffix(u.URL, "/") + "/" + url.PathEscape(name)
	}
	if latest {
		return "https://github.com/" + u.GitHub + "/releases/latest/download/" + url.PathEscape(name)
	}
	return "https://github.com/" + u.GitHub + "/releases/download/" + url.PathEscape(u.TagPrefix+c.Version) + "/" + url.PathEscape(name)
}

// updateFlags are the -ldflags that point the build for target at its
// manifest.
func updateFlags(c *Config, target string) string {
	if c.Updates == nil {
		return ""
	}
	return " -X " + ldflagsQuote("github.com/egoist/mygo.packageUpdateFeed="+c.updateFeed(target)) +
		" -X github.com/egoist/mygo.packageUpdateKey=" + c.Updates.PublicKey
}

// signingKey returns the private key that signs updates, or nil when none
// is available.
func (c *Config) signingKey() (ed25519.PrivateKey, error) {
	text := os.Getenv("MYGO_UPDATER_PRIVATE_KEY")
	if text == "" && c.Updates.PrivateKey != "" {
		path := c.Updates.PrivateKey
		if rest, ok := strings.CutPrefix(path, "~/"); ok {
			home, _ := os.UserHomeDir()
			path = filepath.Join(home, rest)
		}
		b, err := os.ReadFile(c.path(path))
		if err != nil {
			return nil, err
		}
		text = string(b)
	}
	if text == "" {
		return nil, nil
	}
	key, err := update.ParsePrivateKey(text)
	if err != nil {
		return nil, err
	}
	if base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)) != strings.TrimSpace(c.Updates.PublicKey) {
		return nil, fmt.Errorf("the update signing key does not match updates.publicKey in %s", c.configName())
	}
	return key, nil
}

// releaseNotes returns the changelog section of this version.
func (c *Config) releaseNotes() (string, error) {
	name := c.Updates.Changelog
	if name == "" {
		name = "CHANGELOG.md"
		if !fileExists(c.path(name)) {
			return "", nil
		}
	}
	data, err := os.ReadFile(c.path(name))
	if err != nil {
		return "", err
	}
	notes := update.ReleaseNotes(string(data), c.Version)
	if notes == "" {
		return "", fmt.Errorf("%s has no notes for %s: add a \"## %s\" section", name, c.Version, c.Version)
	}
	return notes, nil
}

// writeUpdate archives the entries of stage, the app of target, signs the
// archive and writes its manifest. It returns the files written, none when
// no signing key is available.
func writeUpdate(c *Config, stage, target string, entries []string) ([]string, error) {
	key, err := c.signingKey()
	if err != nil {
		return nil, fmt.Errorf("updates: %w", err)
	}
	if key == nil {
		logf("not signing an update: set MYGO_UPDATER_PRIVATE_KEY or updates.privateKey")
		return nil, nil
	}
	notes, err := c.releaseNotes()
	if err != nil {
		return nil, err
	}
	name := slugify(c.executableName()) + "-" + c.Version + "-" + target + ".tar.gz"
	archive := filepath.Join(stage, name)
	f, err := os.Create(archive)
	if err != nil {
		return nil, err
	}
	sum := sha256.New()
	err = update.WriteArchive(io.MultiWriter(f, sum), stage, entries)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(archive)
	if err != nil {
		return nil, err
	}
	m := update.Manifest{
		Version:   c.Version,
		Notes:     notes,
		Date:      time.Now().UTC().Format(time.RFC3339),
		URL:       c.updateFile(name, false),
		Size:      info.Size(),
		Signature: update.Sign(key, sum.Sum(nil)),
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	manifest := filepath.Join(stage, update.ManifestName(target))
	if err := os.WriteFile(manifest, append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	return []string{archive, manifest}, nil
}

func runKeygen(args []string) error {
	config, _ := os.UserConfigDir()
	flags := newFlags("keygen", "[flags]", `Creates the Ed25519 key pair that signs updates: mygo-update.key, the secret,
and mygo-update.pub. Put the contents of mygo-update.pub in updates.publicKey
of mygo.json, and keep mygo-update.key out of the repository, e.g. in a
password manager or as the MYGO_UPDATER_PRIVATE_KEY secret of CI: installed
apps only accept updates signed with it, so losing it strands them.`)
	out := flags.String("o", filepath.Join(config, "mygo", "update-keys"), "directory to write the keys to")
	force := flags.Bool("force", false, "overwrite existing keys")
	if err := flags.Parse(args); err != nil {
		return err
	}
	secret, public := filepath.Join(*out, "mygo-update.key"), filepath.Join(*out, "mygo-update.pub")
	if !*force && (fileExists(secret) || fileExists(public)) {
		return fmt.Errorf("%s already holds keys; pass -force to replace them", *out)
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(secret, []byte(base64.StdEncoding.EncodeToString(priv)+"\n"), 0o600); err != nil {
		return err
	}
	pubText := base64.StdEncoding.EncodeToString(pub)
	if err := os.WriteFile(public, []byte(pubText+"\n"), 0o644); err != nil {
		return err
	}
	logf("wrote %s (secret) and %s", secret, public)
	fmt.Printf("Add to mygo.json, or mygo.config.ts:\n\n  \"updates\": {\n    \"publicKey\": %q,\n    \"github\": \"owner/name\"\n  }\n", pubText)
	return nil
}
