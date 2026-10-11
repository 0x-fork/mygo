package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

// tileMatch is the identity a tile of the transitions' demo shares with
// its detail view.
type tileMatch struct{ i int }

func init() {
	// A card thrown by the pointer, which a spring lands on the side the
	// throw carries it to.
	var x float32
	right, style := false, 1
	styles := []ui.Animation{ui.Smooth, ui.Snappy, ui.Bouncy}
	demo("motion", func(c *ui.Context) {
		t := c.Theme()
		ui.Segmented(c, &style, "Smooth", "Snappy", "Bouncy")
		const travel = 180
		ui.Box(c).Key("track").Size(300, 90).Radius(12).Border(1, t.Border).Clip().Children(func() {
			card := ui.Box(c).Key("card").Absolute().Left(12).Top(15).Size(96, 60).Center().Radius(10).Background(t.Accent).Cursor(ui.CursorPointer)
			var shown float32
			if dx, _, ok := card.Dragged(); ok {
				x = max(-24, min(x+dx, travel+24))
				shown = card.Jump("x", x)
			} else {
				a := styles[style]
				if vx, _, ok := card.DragEnded(); ok {
					right = x+vx*0.2 > travel/2
					a.Spring.Velocity = vx
				} else if card.Clicked() {
					right = !right
				}
				target := float32(0)
				if right {
					target = travel
				}
				shown = card.AnimateTo("x", target, a)
				x = shown
			}
			card.Translate(shown, 0).Children(func() {
				ui.Text(c, "Throw me").Bold().FontSize(12).TextColor(t.AccentText)
			})
		})
		status(c, "Drag and let go, or click")
	})

	// A card turning over in 3D.
	flipped := false
	demo("transforms", func(c *ui.Context) {
		t := c.Theme()
		stage := ui.Box(c).Key("flip").Size(220, 130)
		turn := float32(0)
		if flipped {
			turn = 180
		}
		turn = stage.AnimateTo("turn", turn, ui.Animation{Spring: ui.SpringOptions{Duration: 600 * time.Millisecond, Bounce: 0.15}})
		stage.Children(func() {
			front := ui.Column(c).Key("front").Absolute().Fill().Center().Gap(4).Radius(14).Background(t.Surface).Border(1, t.Border).Cursor(ui.CursorPointer)
			front.RotateY(turn).Perspective(800).BackfaceHidden().Children(func() {
				ui.Text(c, "Front").Bold()
			})
			back := ui.Column(c).Key("back").Absolute().Fill().Center().Radius(14).Background(t.Accent).Cursor(ui.CursorPointer)
			back.RotateY(turn + 180).Perspective(800).BackfaceHidden().Children(func() {
				ui.Text(c, "Back").Bold().TextColor(t.AccentText)
			})
			if front.Clicked() || back.Clicked() {
				flipped = !flipped
			}
		})
		status(c, "Click the card")
	})

	// Tiles opening into a detail view from where they were.
	open := 0
	colors := []ui.Color{ui.Hex("#2563eb"), ui.Hex("#16a34a"), ui.Hex("#d97706")}
	motion := ui.ElementTransition{Animation: &ui.Animation{Spring: ui.SpringOptions{Duration: 450 * time.Millisecond, Bounce: 0.12}}}
	demo("transitions", func(c *ui.Context) {
		t := c.Theme()
		ui.Box(c).Key("stage").Size(300, 190).Radius(12).Border(1, t.Border).Clip().Padding(12).Children(func() {
			if open == 0 {
				ui.Row(c).Gap(12).Children(func() {
					for i, col := range colors {
						tr := motion
						tr.Match = tileMatch{i}
						if ui.Box(c.Key(i)).Size(64, 64).Radius(12).Background(col).Cursor(ui.CursorPointer).Transition(tr).Clicked() {
							open = i + 1
						}
					}
				})
				return
			}
			i := open - 1
			ui.Column(c).Gap(10).Children(func() {
				tr := motion
				tr.Match = tileMatch{i}
				ui.Box(c.Key("hero")).Size(276, 116).Radius(16).Background(colors[i]).Transition(tr)
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, fmt.Sprintf("Tile %d", i+1)).Bold().Grow(1)
					if ui.Button(c, "Back").Clicked() {
						open = 0
					}
				})
			})
		})
		status(c, "Click a tile")
	})
}
