//go:build darwin

package metal

import (
	"errors"
	"testing"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/gpu/gputest"
	"github.com/egoist/mygo/internal/scene"
)

// BenchmarkRenderSprites draws frames of a 2D game at 1920×1080
// (gputest's SpriteScene: 1,872 sprites of one sheet) into one texture,
// waiting for each: ns/op is the time of a frame, GPU-ms what the GPU
// spent on it, by its command buffer's timestamps.
func BenchmarkRenderSprites(b *testing.B) {
	benchRender(b, gputest.SpriteScene(1920, 1080, 3, 3, 300))
}

// BenchmarkRenderSpritesZoomedOut draws the game zoomed out: about 7,000
// sprites.
func BenchmarkRenderSpritesZoomedOut(b *testing.B) {
	benchRender(b, gputest.SpriteScene(1920, 1080, 1.5, 3, 1000))
}

// BenchmarkRenderEmpty draws frames of nothing at 1920×1080: what the
// others spend besides their sprites.
func BenchmarkRenderEmpty(b *testing.B) {
	benchRender(b, &scene.Scene{Width: 1920, Height: 1080, Clear: scene.Color{A: 255}})
}

var msgDouble func(obj id, sel uintptr) float64

func benchRender(b *testing.B, s *scene.Scene) { benchRenderWith(b, s, nil) }

// benchRenderWith is benchRender with the renderer set up by setup, if not
// nil, and the batches of the frame reported.
func benchRenderWith(b *testing.B, s *scene.Scene, setup func(*Renderer)) {
	r, err := newRenderer()
	if err != nil {
		b.Skip("no Metal:", err)
	}
	defer r.Release()
	if setup != nil {
		setup(r)
	}
	if msgDouble == nil {
		purego.RegisterFunc(&msgDouble, msgSend)
	}
	var tex id
	pool(func() {
		r.cur = &r.formats[0]
		tex = r.newTexture(s.Width, s.Height, pixelFormatBGRA8Unorm, usageRenderTarget|usageShaderRead, nil, 0)
	})
	if tex == 0 {
		b.Fatal("metal: cannot create a target")
	}
	defer pool(func() { release(&tex) })
	gpu := 0.0
	for b.Loop() {
		pool(func() {
			r.waitLast()
			cb, e := r.encode(s, tex)
			if e != nil {
				err = e
				return
			}
			send(cb, "commit")
			send(cb, "waitUntilCompleted")
			if int(send(cb, "status")) == statusError {
				err = errors.New("metal: the frame failed")
				return
			}
			gpu += msgDouble(cb, uintptr(sel("GPUEndTime"))) - msgDouble(cb, uintptr(sel("GPUStartTime")))
		})
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(gpu*1000/float64(b.N), "GPU-ms")
	if setup != nil {
		b.ReportMetric(float64(len(r.b.Batches)), "batches")
	}
}

// BenchmarkRenderBoxes draws 600 rounded boxes casting shadows at
// 1280×800, and BenchmarkRenderTranslatedBoxes the same boxes under
// translations, as an animation moves them.
func BenchmarkRenderBoxes(b *testing.B) { benchRender(b, gputest.BoxesScene(1280, 800, 600, false)) }

func BenchmarkRenderTranslatedBoxes(b *testing.B) {
	benchRender(b, gputest.BoxesScene(1280, 800, 600, true))
}

// BenchmarkRenderCards draws a dashboard of twelve cards with continuous
// corners at 2560×1600 (gputest.CardsScene), each casting two shadows,
// which the builder draws as nine-patches (gpu.Builder.SplitCorners).
func BenchmarkRenderCards(b *testing.B) { benchRender(b, gputest.CardsScene(2560, 1600)) }
