package main

import (
	"slices"

	"github.com/egoist/mygo/ui"
)

// The edge glow's colors, and how its stops move: every shuffleEvery
// seconds they head for places of their own, picked at random, over
// shuffleTime, easing in and out, as the SwiftUI recreations of Apple
// Intelligence's glow move theirs.
var glowColors = []ui.Color{
	ui.Hex("#BC82F3"), ui.Hex("#F5B9EA"), ui.Hex("#8D9FFF"),
	ui.Hex("#FF6778"), ui.Hex("#FFBA71"), ui.Hex("#C686FF"),
}

const shuffleEvery, shuffleTime = 0.5, 1.0

// stop is a stop of the gradient: its color, and where it lies around the
// window, from 0 to 1.
type stop struct {
	r, g, b, at float64
}

// stops are the gradient's stops as they move from where they were (from)
// to where they head (to), since when (since), the next move due at next.
type stops struct {
	from, to    []stop
	since, next float64
	random      func() float64
}

func newStops(random func() float64) stops {
	s := stops{random: random}
	s.to = s.shuffle()
	s.from = s.to
	return s
}

// shuffle returns the colors at places picked at random, in order around
// the window: the stop in each place takes another color as the order
// changes, which the move blends.
func (s *stops) shuffle() []stop {
	out := make([]stop, len(glowColors))
	for i, c := range glowColors {
		out[i] = stop{float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255, s.random()}
	}
	slices.SortFunc(out, func(a, b stop) int {
		switch {
		case a.at < b.at:
			return -1
		case a.at > b.at:
			return 1
		}
		return 0
	})
	return out
}

// step starts the next move once it is due at now, from where the stops
// show.
func (s *stops) step(now float64) {
	if now < s.next {
		return
	}
	s.from = s.show(now)
	s.to, s.since, s.next = s.shuffle(), now, now+shuffleEvery
}

// show returns the stops as they show at now.
func (s *stops) show(now float64) []stop {
	k := ease(min(max((now-s.since)/shuffleTime, 0), 1))
	out := make([]stop, len(s.to))
	for i := range out {
		f, t := s.from[i], s.to[i]
		out[i] = stop{f.r + (t.r-f.r)*k, f.g + (t.g-f.g)*k, f.b + (t.b-f.b)*k, f.at + (t.at-f.at)*k}
	}
	return out
}

// at returns the stops at now as the shader's Stops: a color and a place
// each.
func (s *stops) at(now float64) []float32 {
	var out []float32
	for _, x := range s.show(now) {
		out = append(out, float32(x.r), float32(x.g), float32(x.b), float32(x.at))
	}
	return out
}
