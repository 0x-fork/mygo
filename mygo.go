// Package mygo is a desktop application framework built on the system
// webview (WKWebView on macOS, WebKitGTK on Linux, WebView2 on Windows),
// written in pure Go without cgo.
//
// The API mirrors Electron's main process modules so it feels familiar:
//
//	Electron                     MyGo
//	app                          mygo.App
//	new BrowserWindow(opts)      mygo.NewBrowserWindow(opts)
//	win.webContents              win.WebContents
//	ipcMain                      mygo.IPCMain
//	Menu.buildFromTemplate(t)    mygo.NewMenu(t)
//	dialog / shell / clipboard   mygo.Dialog / mygo.Shell / mygo.Clipboard
//	screen / nativeTheme         mygo.Screen / mygo.NativeTheme
//	protocol.handle(scheme, fn)  mygo.Protocol.Handle(scheme, http.Handler)
//
// A minimal application:
//
//	func main() {
//		app := mygo.App
//		app.WhenReady(func() {
//			win := mygo.NewBrowserWindow(mygo.BrowserWindowOptions{Width: 800, Height: 600})
//			win.LoadURL("https://example.com")
//		})
//		if err := app.Run(); err != nil {
//			log.Fatal(err)
//		}
//	}
//
// # Threading
//
// Native UI toolkits must be driven from the process' main thread. MyGo
// locks the main goroutine to the main thread during package initialization,
// so App.Run must be called from main(). Every other function and method in
// this package is safe to call from any goroutine: calls made off the main
// thread are forwarded to it and wait for the result.
//
// Event listeners (OnClose, OnFocus, ...) run on the main thread; keep them
// short and move slow work to a goroutine. IPC handlers registered with
// IPCMain run on their own goroutines, so they may block.
package mygo

import (
	"runtime"
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

// Version is the MyGo version.
const Version = "0.1.0"

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
