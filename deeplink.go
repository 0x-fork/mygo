package mygo

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

// Set by `mygo build` and `mygo dev` with -ldflags -X: what mygo.json says
// about the app, for platforms whose executables carry no such metadata
// (Linux), and the URL schemes it declares.
var (
	packageName       string
	packageVersion    string
	packageIdentifier string
	packageURLSchemes string // comma separated
)

// packageInfo describes the app as packaged by `mygo build` or `mygo dev`.
func packageInfo() (platform.PackageInfo, bool) {
	if info, ok := backend().App().Package(); ok {
		return info, true
	}
	if packageIdentifier != "" {
		return platform.PackageInfo{Name: packageName, Version: packageVersion, Identifier: packageIdentifier}, true
	}
	return platform.PackageInfo{}, false
}

// Deep links: URLs of the schemes the app handles reach OnOpenURL. macOS
// hands them to bundles that declare the scheme (urlSchemes in mygo.json)
// with Apple Events; Windows and Linux start the app with the URL as an
// argument, so the arguments of the launch, and those that a second
// instance forwards (RequestSingleInstanceLock), are searched for them.

// registeredSchemes are the schemes passed to RegisterURLScheme.
var registeredSchemes struct {
	sync.Mutex
	set map[string]bool
}

var schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*$`)

// handlesScheme reports whether URLs of scheme reach OnOpenURL.
func handlesScheme(scheme string) bool {
	scheme = strings.ToLower(scheme)
	for _, s := range strings.Split(packageURLSchemes, ",") {
		if s != "" && strings.EqualFold(s, scheme) {
			return true
		}
	}
	registeredSchemes.Lock()
	defer registeredSchemes.Unlock()
	return registeredSchemes.set[scheme]
}

// urlArgs returns the command line arguments that are URLs of schemes the
// app handles.
func urlArgs(args []string) []string {
	var urls []string
	for _, a := range args {
		// A single letter is a Windows drive, as in C:\.
		if scheme, _, ok := strings.Cut(a, ":"); ok && len(scheme) > 1 && schemeRe.MatchString(scheme) && handlesScheme(scheme) {
			urls = append(urls, a)
		}
	}
	return urls
}

// deliverURLArgs passes the URLs among args to OnOpenURL.
func deliverURLArgs(args []string) {
	for _, u := range urlArgs(args) {
		fire1(&App.onOpenURL, u)
	}
}

// RegisterURLScheme makes the app the handler of the URLs of scheme for the
// current user, e.g. myapp://open?item=1 for "myapp": they reach OnOpenURL,
// including the one the app is started with. Call it before Run, typically
// every time the app starts: registering again is cheap.
//
// On Windows it registers the executable under HKEY_CURRENT_USER, on Linux
// a hidden desktop entry that becomes the default handler (as xdg-mime
// does). On macOS the scheme must be listed in urlSchemes in mygo.json,
// which registers it with the system; this makes the app its default
// handler when others claim it too. Unregister it when the app is
// uninstalled, for example with UnregisterURLScheme.
func (a *Application) RegisterURLScheme(scheme string) error {
	if !schemeRe.MatchString(scheme) {
		return fmt.Errorf("mygo: invalid URL scheme %q", scheme)
	}
	scheme = strings.ToLower(scheme)
	registeredSchemes.Lock()
	if registeredSchemes.set == nil {
		registeredSchemes.set = map[string]bool{}
	}
	registeredSchemes.set[scheme] = true
	registeredSchemes.Unlock()
	id, name := appID(), a.Name()
	return onMainValue(func() error { return backend().App().RegisterURLScheme(scheme, id, name) })
}

// UnregisterURLScheme undoes RegisterURLScheme (Linux, Windows); URLs of
// the scheme no longer reach the app unless mygo.json declares it.
func (a *Application) UnregisterURLScheme(scheme string) error {
	if !schemeRe.MatchString(scheme) {
		return fmt.Errorf("mygo: invalid URL scheme %q", scheme)
	}
	scheme = strings.ToLower(scheme)
	registeredSchemes.Lock()
	delete(registeredSchemes.set, scheme)
	registeredSchemes.Unlock()
	id, name := appID(), a.Name()
	return onMainValue(func() error { return backend().App().UnregisterURLScheme(scheme, id, name) })
}

// IsURLSchemeRegistered reports whether the system opens URLs of scheme
// with this app.
func (a *Application) IsURLSchemeRegistered(scheme string) bool {
	if !schemeRe.MatchString(scheme) {
		return false
	}
	scheme = strings.ToLower(scheme)
	id, name := appID(), a.Name()
	return onMainValue(func() bool { return backend().App().IsURLSchemeRegistered(scheme, id, name) })
}

// launchURLs delivers the URLs the app was started with, once it is ready.
func launchURLs() { deliverURLArgs(os.Args[1:]) }
