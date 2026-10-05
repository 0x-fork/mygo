package glass

import (
	"testing"

	"github.com/egoist/mygo/internal/gpu"
	"github.com/egoist/mygo/internal/gpu/gputest"
	"github.com/egoist/mygo/internal/gpu/metal"
)

// TestMetal checks that Metal draws the glass as the CPU does.
func TestMetal(t *testing.T) {
	s := testScene()
	pix, err := metal.RenderOffscreen(s)
	if err != nil {
		t.Skip("no Metal:", err)
	}
	gputest.Compare(t, "glass-metal", pix, s.Width*4, s)
	// With continuous corners, as macOS's.
	for i := range s.Ops {
		s.Ops[i].Continuous = true
	}
	if pix, err = metal.RenderOffscreen(s); err != nil {
		t.Fatal(err)
	}
	gputest.Compare(t, "glass-metal-continuous", pix, s.Width*4, s)
}

// TestMetalLibrary checks that the library compiled ahead of time comes
// from glass.metal and the renderer's head and tail as they are.
func TestMetalLibrary(t *testing.T) {
	if gpu.SourceSum(metal.EffectSource(metalSource)) != metalSum {
		t.Fatal("glass.metal or the Metal renderer's effect.metal changed since shaders_darwin.go was generated: run go generate ./plugins/glass on macOS")
	}
}
