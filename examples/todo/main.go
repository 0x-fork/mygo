// Todo is a small but complete MyGo app: typed services and events shared
// with a TypeScript frontend, persistence, dialogs, menus and multiple
// windows kept in sync.
//
//	go run ./cmd/mygo dev examples/todo    # live reload with the Vite dev server
//	go run ./cmd/mygo build examples/todo  # the app and a disk image in examples/todo/build
package main

import (
	"log"

	"github.com/egoist/mygo"
)

func main() {
	app := mygo.App
	// Set the name first: it determines the user data directory.
	app.SetName("Todo")

	store, err := OpenStore()
	if err != nil {
		log.Fatal(err)
	}
	mygo.Bind(store)

	app.WhenReady(func() {
		app.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Role: mygo.RoleAppMenu},
			{Label: "File", Submenu: []*mygo.MenuItem{
				{Label: "New Window", Accelerator: "CmdOrCtrl+N", Click: func(*mygo.MenuItem, *mygo.Window) { openWindow() }},
				{Label: "Export…", Accelerator: "CmdOrCtrl+E", Click: func(_ *mygo.MenuItem, w *mygo.Window) {
					go store.export(w)
				}},
				mygo.Separator(),
				{Role: mygo.RoleClose},
			}},
			{Role: mygo.RoleEditMenu},
			{Role: mygo.RoleViewMenu},
			{Role: mygo.RoleWindowMenu},
		}))
		openWindow()
	})
	// Keep the app alive in the Dock and reopen a window when clicked.
	app.OnWindowAllClosed(func() {})
	app.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			openWindow()
		}
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func openWindow() {
	mygo.NewWindow(mygo.WindowOptions{
		Title:           "Todo",
		Width:           460,
		Height:          640,
		MinWidth:        360,
		MinHeight:       420,
		TitleBarStyle:   mygo.TitleBarHidden,
		BackgroundColor: "#f5f5f7",
		URL:             "/", // the frontend: devUrl in development, frontendDist once built
	})
}
