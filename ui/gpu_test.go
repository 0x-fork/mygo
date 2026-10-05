package ui

import (
	"errors"
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/surface"
)

// testSurface is a surface that counts the frames drawn in memory.
type testSurface struct{ pixels int }

func (s *testSurface) Native() platform.SurfaceNative           { return platform.SurfaceNative{HWND: 1} }
func (s *testSurface) Size() (float64, float64, float64)        { return 200, 100, 1 }
func (s *testSurface) RequestFrame()                            {}
func (s *testSurface) RefreshRate() float64                     { return 60 }
func (s *testSurface) PresentPixels([]byte, int, int, int)      { s.pixels++ }
func (s *testSurface) SetCursor(platform.Cursor)                {}
func (s *testSurface) SetTextInput(platform.TextInputState)     {}
func (s *testSurface) UpdateAccessibility(*platform.AccessTree) {}

// testGPU is a GPU renderer whose device goes away when fail is set.
type testGPU struct {
	fail             bool
	frames, released int
}

func (g *testGPU) Render(*scene.Scene) error {
	if g.fail {
		return errors.New("the device was removed")
	}
	g.frames++
	return nil
}

func (g *testGPU) Release() { g.released++ }

// gpuHost returns a window host on a test surface whose GPU renderers
// newGPU makes, and a function drawing a frame.
func gpuHost(t *testing.T, make func() (gpuRenderer, error)) (*windowHost, *testSurface, func()) {
	t.Setenv("MYGO_GPU", "")
	newGPU = func(platform.SurfaceNative) (gpuRenderer, error) { return make() }
	t.Cleanup(func() { newGPU = newGPURenderer })
	s := &testSurface{}
	h := &windowHost{conn: &surface.Conn{Surface: s}}
	h.rt = newRuntime(func(c *Context) { Text(c, "Hello") }, h)
	return h, s, func() { h.event(platform.SurfaceEvent{Kind: platform.SurfaceFrame}) }
}

// softGPU is a renderer drawing on the CPU, as Direct3D's WARP.
type softGPU struct{ testGPU }

func (g *softGPU) Software() bool { return true }

func TestGPUComesBack(t *testing.T) {
	var made []*testGPU
	gone := false
	h, s, frame := gpuHost(t, func() (gpuRenderer, error) {
		if gone {
			return nil, errors.New("no device")
		}
		g := &testGPU{}
		made = append(made, g)
		return g, nil
	})
	frame()
	if len(made) != 1 || made[0].frames != 1 || s.pixels != 0 {
		t.Fatalf("first frame: %d renderers, %d frames in memory", len(made), s.pixels)
	}

	// A renderer that fails gives way to another at once.
	made[0].fail = true
	frame()
	if made[0].released != 1 || len(made) != 2 || made[1].frames != 1 || s.pixels != 0 {
		t.Fatalf("after a failure: released %d, %d renderers, %d frames in memory", made[0].released, len(made), s.pixels)
	}

	// Without a device, frames are drawn in memory until the wait is over.
	gone = true
	made[1].fail = true
	frame()
	frame()
	if len(made) != 2 || s.pixels != 2 || h.backoff != time.Second {
		t.Fatalf("without a device: %d renderers, %d frames in memory, wait %v", len(made), s.pixels, h.backoff)
	}
	// None either when the wait is over: the next wait is longer.
	h.retryAt = time.Now()
	frame()
	if s.pixels != 3 || h.backoff != 2*time.Second || h.retryAt.IsZero() {
		t.Fatalf("still without a device: %d frames in memory, wait %v", s.pixels, h.backoff)
	}
	// It is back.
	gone = false
	h.retryAt = time.Now()
	frame()
	if len(made) != 3 || made[2].frames != 1 || s.pixels != 3 || !h.retryAt.IsZero() {
		t.Fatalf("once back: %d renderers, %d frames in memory", len(made), s.pixels)
	}
}

func TestSoftwareUntilTheGPUIsBack(t *testing.T) {
	onGPU := true
	var gpus []*testGPU
	var soft *softGPU
	h, s, frame := gpuHost(t, func() (gpuRenderer, error) {
		if onGPU {
			g := &testGPU{}
			gpus = append(gpus, g)
			return g, nil
		}
		soft = &softGPU{}
		return soft, nil
	})
	frame()
	// The GPU goes away: a renderer drawing in software takes its place,
	// and frames still show.
	onGPU = false
	gpus[0].fail = true
	frame()
	if soft == nil || soft.frames != 1 || s.pixels != 0 || !h.degraded || h.retryAt.IsZero() {
		t.Fatalf("after the loss: software %v, %d frames in memory, degraded %v", soft != nil, s.pixels, h.degraded)
	}
	// When the wait is over, a frame tries for the GPU, still away.
	first := soft
	h.retryAt = time.Now()
	frame()
	if first.released != 1 || soft == first || soft.frames != 1 || h.backoff != 2*time.Second {
		t.Fatalf("trying again: released %d, wait %v", first.released, h.backoff)
	}
	// It is back.
	onGPU = true
	h.retryAt = time.Now()
	frame()
	if soft.released != 1 || len(gpus) != 2 || gpus[1].frames != 1 || h.degraded || !h.retryAt.IsZero() {
		t.Errorf("once back: software released %d, %d GPU renderers, degraded %v", soft.released, len(gpus), h.degraded)
	}
}

func TestNoGPUFromTheStart(t *testing.T) {
	tries := 0
	h, s, frame := gpuHost(t, func() (gpuRenderer, error) {
		tries++
		return nil, errors.New("no device")
	})
	frame()
	frame()
	if tries != 1 || s.pixels != 2 || !h.retryAt.IsZero() {
		t.Errorf("%d tries, %d frames in memory, retry at %v", tries, s.pixels, h.retryAt)
	}

	// A machine whose renderer draws in software from the start keeps it.
	tries = 0
	h, s, frame = gpuHost(t, func() (gpuRenderer, error) {
		tries++
		return &softGPU{}, nil
	})
	frame()
	frame()
	if tries != 1 || s.pixels != 0 || h.degraded || !h.retryAt.IsZero() {
		t.Errorf("software from the start: %d tries, degraded %v, retry at %v", tries, h.degraded, h.retryAt)
	}
}

// pixelGPU is a GPU renderer that also presents frames drawn in memory, as
// Metal's does.
type pixelGPU struct {
	testGPU
	pixels int
	damage []image.Rectangle
}

func (g *pixelGPU) PresentPixels(pix []byte, stride, width, height int, scale float64, damage []image.Rectangle) error {
	g.pixels++
	g.damage = append(g.damage[:0], damage...)
	return nil
}

// TestSmallChangesDrawOnCPU checks which frames the CPU draws and which
// the GPU does.
func TestSmallChangesDrawOnCPU(t *testing.T) {
	g := &pixelGPU{}
	h, s, frame := gpuHost(t, func() (gpuRenderer, error) { return g, nil })
	x, back := float32(10), RGB(200, 200, 200)
	h.rt = newRuntime(func(c *Context) {
		Box(c).Fill().Background(back).Children(func() {
			Box(c).Size(10, 10).Background(RGB(0, 0, 255)).Absolute().Left(x).Top(10)
		})
	}, h)
	pause := func() { h.lastFrame = time.Now().Add(-time.Second) }
	check := func(what string, pixels, frames int) {
		t.Helper()
		if g.pixels != pixels || g.frames != frames || s.pixels != 0 {
			t.Fatalf("%s: %d frames drawn on the CPU, %d on the GPU, %d in memory without the GPU", what, g.pixels, g.frames, s.pixels)
		}
	}
	frame()
	check("the first frame", 1, 0)
	if len(g.damage) != 1 || g.damage[0] != image.Rect(0, 0, 200, 100) {
		t.Errorf("the first frame changed %v", g.damage)
	}
	// A small change after a pause, and in a burst.
	pause()
	x = 30
	frame()
	check("a small change after a pause", 2, 0)
	if len(g.damage) == 0 || g.damage[0].Dx() > 40 || g.damage[0].Dy() > 20 {
		t.Errorf("moving the square changed %v", g.damage)
	}
	x = 40
	frame()
	check("a small change in a burst", 3, 0)
	// Much of the window in a burst goes to the GPU, and back to the CPU
	// after a pause.
	back = RGB(100, 100, 100)
	frame()
	check("a large change in a burst", 3, 1)
	pause()
	x = 50
	frame()
	check("a small change after a pause, the CPU's frame being old", 4, 1)
	if len(g.damage) != 1 || g.damage[0] != image.Rect(0, 0, 200, 100) {
		t.Errorf("catching up changed %v", g.damage)
	}
	// The CPU's frame goes once the GPU has drawn alone for a second.
	back = RGB(50, 50, 50)
	frame()
	check("a large change", 4, 2)
	if h.soft.Image.Pix == nil {
		t.Fatal("the CPU's frame went at once")
	}
	h.gpuSinceCPU = time.Now().Add(-2 * time.Second)
	back = RGB(60, 60, 60)
	frame()
	check("a large change, a second later", 4, 3)
	if h.soft.Image.Pix != nil {
		t.Error("the CPU's frame stays while the GPU draws alone")
	}
	pause()
	frame()
	check("the next frame after a pause", 5, 3)
}
