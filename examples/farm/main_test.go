package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Tests play no sound: the game is made without its sounds.

// The farmer tills, plants, waters, and harvests what grows.
func TestFarming(t *testing.T) {
	w := newWorld()
	w.facing = left
	tx, ty := w.target()
	tile := &w.tiles[ty][tx]
	if tile.ground != grass || tile.object != none {
		t.Fatalf("the farmer starts facing %v", *tile)
	}
	w.tool = hoe
	w.use()
	w.tool = seeds
	w.use()
	w.tool = can
	w.use()
	if tile.ground != soil || tile.crop != 0 || !tile.wet {
		t.Fatalf("after hoe, seeds and water: %+v", *tile)
	}
	// Watered, it grows a stage, then waits for water again.
	for range int(growTime/stepTime) + 2 {
		w.step(stepTime)
	}
	if tile.crop != 1 || tile.wet {
		t.Fatalf("after growing a stage: %+v", *tile)
	}
	for tile.crop < ripe {
		w.use()
		for range int(growTime/stepTime) + 2 {
			w.step(stepTime)
		}
	}
	w.tool = hoe
	w.use()
	if tile.crop != -1 || w.coins != 15 {
		t.Errorf("after harvesting: crop %d, %d coins", tile.crop, w.coins)
	}
	want := []event{evTill, evPlant, evWater, evWater, evWater, evHarvest}
	if len(w.events) != len(want) {
		t.Errorf("events %v, want %v", w.events, want)
	}
}

// The farmer walks, and stops at the pond and the trees.
func TestWalking(t *testing.T) {
	w := newWorld()
	w.held[right] = true
	for range 5 * 120 {
		w.step(stepTime)
	}
	if w.facing != right || w.x <= 21 {
		t.Errorf("after walking right: at %v facing %v", w.x, w.facing)
	}
	if w.x > mapW-2 {
		t.Errorf("walked into the trees: %v", w.x)
	}
	w.held = [4]bool{up: true}
	for range 20 * 120 {
		w.step(stepTime)
	}
	if w.y < 2 {
		t.Errorf("walked into the trees north: %v", w.y)
	}
	if len(w.events) == 0 || w.events[0] != evStep {
		t.Error("no footsteps")
	}
}

// The game's view draws, takes keys, and walks the farmer while they are
// held.
func TestView(t *testing.T) {
	g := newGame(false)
	tt := ui.NewTester(g.view, 960, 640)
	x := g.w.x
	tt.Key(0, ui.Key3)
	if g.w.tool != can {
		t.Errorf("key 3 chose %v", g.w.tool)
	}
	tt.Key(0, ui.KeyEqual)
	if g.zoom != 3 {
		t.Errorf("zoom %d after +", g.zoom)
	}
	g.w.held[right] = true
	for range 30 {
		g.w.step(stepTime)
	}
	tt.Frame()
	if g.w.x <= x {
		t.Error("the farmer did not walk")
	}
}
