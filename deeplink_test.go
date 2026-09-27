package mygo

import (
	"slices"
	"testing"
)

func TestURLSchemes(t *testing.T) {
	defer func(s string) { packageURLSchemes = s }(packageURLSchemes)
	packageURLSchemes = "myapp,Other"
	if err := App.RegisterURLScheme("Run.Time+x"); err != nil {
		t.Fatal(err)
	}
	defer App.UnregisterURLScheme("run.time+x")
	for _, bad := range []string{"", "1abc", "a b", "a:b"} {
		if err := App.RegisterURLScheme(bad); err == nil {
			t.Errorf("RegisterURLScheme(%q) succeeded", bad)
		}
	}
	id := urlHandlerID()
	if got := fb.URLSchemes["run.time+x"]; got != id+" "+App.Name() {
		t.Errorf("registered %q", got)
	}
	if !App.IsURLSchemeRegistered("run.time+x") || App.IsURLSchemeRegistered("myapp") {
		t.Error("IsURLSchemeRegistered")
	}

	args := []string{"--flag", "MyApp://a", "other:b", "run.time+x://c", "https://example.com", `C:\file.txt`, "nope://d", "myapp"}
	if got, want := urlArgs(args), []string{"MyApp://a", "other:b", "run.time+x://c"}; !slices.Equal(got, want) {
		t.Errorf("urlArgs = %q, want %q", got, want)
	}
	var opened []string
	off := App.OnOpenURL(func(u string) { opened = append(opened, u) })
	onMain(func() { deliverURLArgs(args) })
	off()
	if len(opened) != 3 {
		t.Errorf("OnOpenURL got %q", opened)
	}

	if err := App.UnregisterURLScheme("run.time+x"); err != nil {
		t.Fatal(err)
	}
	if handlesScheme("run.time+x") || App.IsURLSchemeRegistered("run.time+x") {
		t.Error("the scheme is still handled after UnregisterURLScheme")
	}
}

func TestPackageInfo(t *testing.T) {
	defer func(n, v, id string) { packageName, packageVersion, packageIdentifier = n, v, id }(packageName, packageVersion, packageIdentifier)
	if _, ok := packageInfo(); ok {
		t.Fatal("the test binary is not packaged")
	}
	packageName, packageVersion, packageIdentifier = "My App", "1.2.3", "com.example.app"
	if info, ok := packageInfo(); !ok || info.Name != "My App" || info.Version != "1.2.3" || !App.IsPackaged() {
		t.Errorf("linked package info = %+v %v", info, ok)
	}
	if urlHandlerID() != "com.example.app" {
		t.Errorf("handler id = %q", urlHandlerID())
	}
}
