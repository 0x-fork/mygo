// Package mygo is a desktop application framework built on the system
// webview (WKWebView on macOS, WebKitGTK on Linux, WebView2 on Windows),
// written in pure Go without cgo.
//
// An app binds Go services, whose exported methods its web frontend calls
// through a TypeScript client that `mygo generate` writes, and opens
// windows once it is ready:
//
//	type Greeter struct{}
//
//	// Greet returns a greeting.
//	func (Greeter) Greet(name string) string { return "Hello, " + name }
//
//	func main() {
//		mygo.Bind(Greeter{})
//		mygo.App.WhenReady(func() {
//			mygo.NewWindow(mygo.WindowOptions{Title: "Hello", URL: "/"})
//		})
//		if err := mygo.App.Run(); err != nil {
//			log.Fatal(err)
//		}
//	}
//
// The frontend then calls `await Greeter.greet("Ada")`. Go reaches pages
// with typed events declared with NewEvent.
//
// The rest of the package covers the desktop: App (the lifecycle, deep
// links, file associations, the Dock), Window, NewMenu and NewTray, Dialog,
// NewNotification, Clipboard, Shell, Screen, Theme, Power, GlobalShortcut,
// Protocol (custom URL schemes served by an http.Handler) and Updater.
// The guides in the repository's docs directory show how they fit
// together.
//
// # Threading
//
// Native UI toolkits must be driven from the process' main thread. MyGo
// locks the main goroutine to the main thread during package initialization,
// so App.Run must be called from main(). Every other function and method in
// this package is safe to call from any goroutine: calls made off the main
// thread are forwarded to it and wait for the result.
//
// Before App.Run, main sets the app up: it binds services, adds listeners
// and makes settings, such as Theme.SetSource, which apply once the app
// starts. Calls that need the running app, such as the clipboard, displays
// or dialogs, panic when main makes them before Run; other goroutines wait
// for the app to start.
//
// Event listeners (OnClose, OnFocus, ...) run on the main thread; keep them
// short and move slow work to a goroutine. Methods of bound services run on
// goroutines of their own, one per call, so they may block.
package mygo

import (
	"runtime"
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

// Version is the MyGo version.
const Version = "0.1.9"

func init() {
	// Cocoa, GTK and Win32 all require the UI to live on the thread that
	// started the process. Keep the main goroutine there.
	runtime.LockOSThread()
}

var (
	backendOnce sync.Once
	theBackend  platform.Backend
)

// backend returns the platform backend, creating it on first use.
func backend() platform.Backend {
	backendOnce.Do(func() { theBackend = newBackend() })
	return theBackend
}
