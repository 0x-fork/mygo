// Counter-native is a counter in native UI: a window MyGo draws itself from
// Go, with no web page. The arrow keys count too.
//
//	go run ./examples/counter-native
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// counter is the state the window shows.
type counter struct{ n int }

// view builds the interface from the state, on the main thread, whenever the
// window needs a frame.
func (s *counter) view(c *ui.Context) {
	c.OnShortcut(0, ui.KeyUp, func() { s.n++ })
	c.OnShortcut(0, ui.KeyDown, func() { s.n-- })
	t := c.Theme()
	ui.Column(c).Fill().Center().Gap(16).Children(func() {
		ui.Textf(c, "%d", s.n).FontSize(56).Bold()
		ui.Row(c).Gap(8).Children(func() {
			ui.Button(c.Key("decrement"), "−").Label("Decrement").Width(44).OnClick(func() { s.n-- })
			ui.Button(c.Key("reset"), "Reset").Disabled(s.n == 0).OnClick(func() { s.n = 0 })
			ui.PrimaryButton(c.Key("increment"), "+").Label("Increment").Width(44).OnClick(func() { s.n++ })
		})
		ui.Text(c, "↑ and ↓ count too").FontSize(12).TextColor(t.TextMuted)
	})
}

func main() {
	s := &counter{}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "Counter",
			Width:   360,
			Height:  260,
			Content: ui.View(s.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
