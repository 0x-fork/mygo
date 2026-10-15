// Chat-glow is an assistant's window, lit as Apple Intelligence lights its
// own, by shaders written in Go (package ui/shader): while the assistant
// works, the window's edges glow in flowing colors after a wave of light
// runs in from them, the field to write in glows in its ring, and
// "Thinking…" shimmers; the assistant's orb, a glass sphere of drifting
// light, quickens as it thinks.
//
//	go run ./examples/chat-glow
//
// Each is a pattern of its own for an app: a glow over the whole window
// that the pointer goes through (an Absolute, PassThrough element drawn
// last), a glowing focus ring drawn around a field, text filled with a
// shader (shader.Text), and an avatar drawn by one. The replies are
// canned; Escape stops one.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	a := newApp()
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:         "Chat Glow",
			Width:         860,
			Height:        700,
			TitleBarStyle: mygo.TitleBarHiddenInset,
			Content:       ui.View(a.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
