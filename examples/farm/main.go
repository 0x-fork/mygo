// Farm is a little farming game in native UI: walk a farm with the
// keyboard, till the grass, plant seeds, water them and harvest what
// grows, through a camera that follows you, as days turn to night, with
// sound and music. Everything it draws and plays is made by its code:
// pixel art in a pixelated sheet drawn with Painter.ImageRegion, under
// Painter.Transform, and sounds synthesized for package audio.
//
//	go run ./examples/farm
//	go run ./cmd/mygo dev -web examples/farm
//
// Arrows or WASD walk, Space or E uses the tool (or harvests what is
// ripe), 1 2 3 or Q choose the tool, + and - zoom, and M plays or stops
// the music.
package main

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// stepTime is the world's fixed step: 120 a second, whatever the frame
// rate, so that the game plays the same on every display.
const stepTime = 1.0 / 120

// zooms are the camera's zooms: pixels of the screen per pixel of art, in
// DIPs. Not all are whole numbers: pixelated sampling keeps the art's
// pixels square at any.
var zooms = []float64{2, 2.5, 3, 4, 5}

type game struct {
	w     *world
	art   sprites
	snd   *sounds // nil in tests
	zoom  int
	music bool
	// keys are the keys held, by position, which hold the directions.
	keys map[ui.Key]bool
	// last is when the world last advanced, and lag the time it owes the
	// fixed steps.
	last time.Time
	lag  float64
}

func newGame(withSound bool) *game {
	g := &game{w: newWorld(), art: newSprites(), zoom: 2, keys: map[ui.Key]bool{}}
	if withSound {
		g.snd = newSounds()
		g.setMusic(true)
	}
	return g
}

func (g *game) setMusic(on bool) {
	g.music = on
	if g.snd == nil {
		return
	}
	if on {
		g.snd.music.Play()
	} else {
		g.snd.music.Pause()
	}
}

func (g *game) view(c *ui.Context) {
	board := ui.Box(c).Fill().Background(ui.Hex("#4b9a2a")).
		Focusable().Focus().Label("Farm").
		HandleInput(g.input).
		Draw(g.draw)
	// Keys released while the window is in the background never come.
	if !board.Focused() {
		clear(g.keys)
		g.w.held = [4]bool{}
	}
}

// directions maps the keys that walk to their directions.
var directions = map[ui.Key]dir{
	ui.KeyUp: up, ui.KeyW: up, ui.KeyDown: down, ui.KeyS: down,
	ui.KeyLeft: left, ui.KeyA: left, ui.KeyRight: right, ui.KeyD: right,
}

func (g *game) input(ev ui.InputEvent) bool {
	if ev.Kind != ui.InputKeyDown && ev.Kind != ui.InputKeyUp {
		return false
	}
	if ev.Mods&(ui.Super|ui.Ctrl|ui.Alt) != 0 {
		return false // the menus' shortcuts
	}
	k := ev.PhysicalKey
	if k == ui.KeyUnknown {
		k = ev.Key
	}
	pressed := ev.Kind == ui.InputKeyDown
	if _, ok := directions[k]; ok {
		g.keys[k] = pressed
		g.w.held = [4]bool{}
		for key, held := range g.keys {
			if held {
				g.w.held[directions[key]] = true
			}
		}
		return true
	}
	if !pressed || ev.Repeat {
		return false
	}
	switch k {
	case ui.KeySpace, ui.KeyE, ui.KeyEnter:
		g.w.use()
	case ui.Key1, ui.Key2, ui.Key3:
		g.w.tool = tool(k - ui.Key1)
	case ui.KeyQ, ui.KeyTab:
		g.w.tool = (g.w.tool + 1) % 3
	case ui.KeyEqual:
		g.zoom = min(g.zoom+1, len(zooms)-1)
	case ui.KeyMinus:
		g.zoom = max(g.zoom-1, 0)
	case ui.KeyM:
		g.setMusic(!g.music)
	default:
		return false
	}
	return true
}

// advance runs the world's fixed steps up to now, and plays what happened.
// Draw functions may run more than once a frame: a second time at the same
// now runs none.
func (g *game) advance(now time.Time) {
	if !g.last.IsZero() {
		// After a pause, as a window out of sight, not all of it.
		g.lag += min(now.Sub(g.last).Seconds(), 0.25)
	}
	g.last = now
	for g.lag >= stepTime {
		g.w.step(stepTime)
		g.lag -= stepTime
	}
	if g.snd != nil && len(g.w.events) > 0 {
		g.snd.play(g.w.events, 0)
	}
	g.w.events = g.w.events[:0]
}

// tint is the color the world takes at the light of the hour: the night
// is blue.
func (g *game) tint() ui.Color {
	light, _ := g.w.daylight()
	mix := func(night, day float64) uint8 { return uint8(night + (day-night)*light) }
	return ui.RGB(mix(80, 255), mix(95, 255), mix(160, 255))
}

func (g *game) draw(p *ui.Painter, r ui.Rect) {
	p.AnimationFrame()
	g.advance(p.Now())
	w := g.w
	zoom := zooms[g.zoom]
	// The camera follows the farmer, within the map, and moves by whole
	// pixels of the screen so that the tiles' edges stay on them.
	viewW, viewH := float64(r.W)/zoom, float64(r.H)/zoom
	camX := clampCamera(w.x*cell-viewW/2, mapW*cell, viewW)
	camY := clampCamera(w.y*cell-viewH/2, mapH*cell, viewH)
	px := zoom * float64(p.Scale())
	camX, camY = math.Round(camX*px)/px, math.Round(camY*px)/px
	cam := ui.Translation(float32(-camX), float32(-camY)).Then(ui.Scaling(float32(zoom), float32(zoom)))
	tint := g.tint()
	p.Clip(r, 0, func() {
		p.Transform(cam, r.X, r.Y, func() {
			g.drawWorld(p, r, tint)
		})
	})
	g.drawHUD(p, r)
}

func clampCamera(c, world, view float64) float64 {
	if world <= view {
		return (world - view) / 2
	}
	return min(max(c, 0), world-view)
}

// drawWorld draws the map in the art's pixels from r's corner: the ground,
// crops, then what stands, row by row with the farmer among them, so that
// what stands south of another covers it.
func (g *game) drawWorld(p *ui.Painter, r ui.Rect, tint ui.Color) {
	w := g.w
	put := func(i int, x, y float32) {
		s := g.art.cell(i)
		s.tint = tint
		g.art.draw(p, s, ui.Rect{X: r.X + x, Y: r.Y + y, W: cell, H: cell})
	}
	waterFrame := cWater0
	if int(w.now*2)%2 == 1 {
		waterFrame = cWater1
	}
	for y := range mapH {
		for x := range mapW {
			t := &w.tiles[y][x]
			fx, fy := float32(x*cell), float32(y*cell)
			switch t.ground {
			case grass:
				put([]int{cGrass, cGrass2, cFlowers}[t.variant], fx, fy)
			case path:
				put(cPath, fx, fy)
			case soil:
				if t.wet {
					put(cWetSoil, fx, fy)
				} else {
					put(cSoil, fx, fy)
				}
			case water:
				put(waterFrame, fx, fy)
			}
			if t.crop >= 0 {
				put(cSeed+int(t.crop), fx, fy)
			}
		}
	}
	// The tile the tool would act on.
	if tx, ty := w.target(); tx >= 0 && ty >= 0 && tx < mapW && ty < mapH {
		put(cFrame, float32(tx*cell), float32(ty*cell))
	}
	feet := int(math.Floor(w.y))
	for y := range mapH {
		for x := range mapW {
			fx, fy := float32(x*cell), float32(y*cell)
			switch w.tiles[y][x].object {
			case tree:
				put(cTreeTrunk, fx, fy)
				put(cTreeTop, fx, fy-cell+4)
			case bush:
				put(cBush, fx, fy)
			case rock:
				put(cRock, fx, fy)
			case fence:
				put(cFence, fx, fy)
			}
		}
		if y == feet {
			g.art.draw(p, g.farmer(tint), ui.Rect{X: r.X + float32(w.x*cell) - cell/2, Y: r.Y + float32(w.y*cell) - cell + 2, W: cell, H: cell})
		}
	}
}

// farmer returns the farmer's sprite: their frame of walking, facing
// where they go, flipped to face left. They move smoothly: under the
// camera's transform, sprites are where they are, not on whole pixels.
func (g *game) farmer(tint ui.Color) sprite {
	w := g.w
	frame := 0
	if w.moving {
		frame = 1 + int(w.walked/0.35)%2
	}
	var s sprite
	switch w.facing {
	case down:
		s = g.art.cell(cDown + frame)
	case up:
		s = g.art.cell(cUp + frame)
	default:
		s = g.art.cell(cSide + frame)
		s.flipX = w.facing == left
	}
	s.tint = tint
	return s
}

// drawHUD draws, over the world, the coins and the hour, and the toolbar.
func (g *game) drawHUD(p *ui.Painter, r ui.Rect) {
	w := g.w
	panel := ui.RGBA(20, 18, 30, 0.75)
	white, muted := ui.RGB(255, 255, 255), ui.RGB(200, 200, 215)
	_, hour := w.daylight()
	p.Fill(ui.Rect{X: r.X + 12, Y: r.Y + 12, W: 168, H: 40}, panel, 10)
	g.art.draw(p, g.art.cell(cCoin), ui.Rect{X: r.X + 18, Y: r.Y + 16, W: 32, H: 32})
	p.Text(r.X+54, r.Y+21, fmt.Sprint(w.coins), 18, white)
	p.Text(r.X+110, r.Y+23, fmt.Sprintf("%02d:00", hour), 15, muted)
	// The toolbar, at the bottom, the tool in use framed.
	const slot, gap = 52, 8
	bw := float32(3*slot + 2*gap + 16)
	bx, by := r.X+(r.W-bw)/2, r.Y+r.H-slot-50
	p.Fill(ui.Rect{X: bx, Y: by, W: bw, H: slot + 16}, panel, 12)
	for i, icon := range []int{cHoe, cSeeds, cCan} {
		x := bx + 8 + float32(i)*(slot+gap)
		box := ui.Rect{X: x, Y: by + 8, W: slot, H: slot}
		if tool(i) == w.tool {
			p.Fill(box, ui.RGBA(255, 255, 255, 0.15), 8)
			p.Stroke(box, ui.RGB(251, 242, 54), 8, 2)
		}
		g.art.draw(p, g.art.cell(icon), ui.Rect{X: x + 4, Y: by + 12, W: slot - 8, H: slot - 8})
		p.Text(x+4, by+10, fmt.Sprint(i+1), 11, muted)
	}
	hint := toolNames[w.tool] + " · Space uses it · 1 2 3 or Q switch · + − zoom · M music"
	label := ui.Span{Text: hint, Size: 12, Color: muted}
	tw, th := p.MeasureText(0, label)
	p.Fill(ui.Rect{X: r.X + (r.W-tw)/2 - 10, Y: r.Y + r.H - 30, W: tw + 20, H: th + 10}, panel, (th+10)/2)
	p.RichText(r.X+(r.W-tw)/2, r.Y+r.H-25, 0, label)
}

func main() {
	g := newGame(true)
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "Farm",
			Width:   960,
			Height:  640,
			Content: ui.View(g.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
