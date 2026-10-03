//go:build darwin

package metal

import (
	"image"
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/gpu"
	"github.com/egoist/mygo/internal/gpu/gputest"
)

func TestDrawsAsTheCPURenderer(t *testing.T) {
	r, err := newRenderer()
	if err != nil {
		t.Skip("no Metal:", err)
	}
	defer r.Release()
	s := gputest.Scene()
	// Twice: the second frame updates what the first uploaded.
	for range 2 {
		pix, err := r.renderOffscreen(s)
		if err != nil {
			t.Fatal(err)
		}
		gputest.Compare(t, "metal", pix, s.Width*4, s)
	}
}

// TestShaderLibrary checks that the library compiled ahead of time comes
// from shader.metal as it is, and that Metal loads it.
func TestShaderLibrary(t *testing.T) {
	if gpu.SourceSum(shaderSource) != shaderLibrarySum {
		t.Fatal("shader.metal changed since shaderlib.go was generated: run go generate ./internal/gpu/metal on macOS")
	}
	r, err := newRenderer()
	if err != nil {
		t.Skip("no Metal:", err)
	}
	defer r.Release()
	var lib id
	pool(func() { lib = r.compiledLibrary() })
	if lib == 0 {
		t.Fatal("Metal does not load the compiled library")
	}
	pool(func() { release(&lib) })
}

// TestChangedSince checks what frames drawn in memory copy into the
// drawables, which take turns, and after the GPU drew into one.
func TestChangedSince(t *testing.T) {
	r := &Renderer{shown: map[uint32]uint64{}}
	whole := []image.Rectangle{image.Rect(0, 0, 100, 50)}
	present := func(key uint32, damage ...image.Rectangle) []image.Rectangle {
		r.pixelFrames++
		n := r.pixelFrames
		r.pixelDamage[n%pixelHistory] = append(r.pixelDamage[n%pixelHistory][:0], damage...)
		rects := r.changedSince(key, n, 100, 50)
		r.shown[key] = n
		return rects
	}
	a, b := image.Rect(0, 0, 10, 10), image.Rect(20, 0, 30, 10)
	if got := present(1, whole...); !slices.Equal(got, whole) {
		t.Errorf("a new drawable: %v", got)
	}
	if got := present(2, a); !slices.Equal(got, whole) {
		t.Errorf("the other new drawable: %v", got)
	}
	// Each drawable lags two frames behind.
	if got := present(1, b); !slices.Equal(got, []image.Rectangle{a, b}) {
		t.Errorf("the first drawable again: %v", got)
	}
	// The GPU draws into the second.
	delete(r.shown, 2)
	if got := present(2, a); !slices.Equal(got, whole) {
		t.Errorf("a drawable the GPU drew: %v", got)
	}
	// A drawable left behind for longer than the history.
	for range pixelHistory {
		present(2, a)
	}
	if got := present(1, b); !slices.Equal(got, whole) {
		t.Errorf("a drawable %d frames behind: %v", pixelHistory+1, got)
	}
}

func TestCopyRect(t *testing.T) {
	src := make([]byte, 4*4*3) // 4×3, stride 16
	for i := range src {
		src[i] = byte(i)
	}
	dst := make([]byte, 32*3) // stride 32, wider than the frame
	copyRect(dst, 32, src, 16, image.Rect(1, 1, 3, 3))
	for y := range 3 {
		for x := range 4 {
			in := y >= 1 && y < 3 && x >= 1 && x < 3
			for c := range 4 {
				got, want := dst[y*32+x*4+c], byte(0)
				if in {
					want = src[y*16+x*4+c]
				}
				if got != want {
					t.Fatalf("pixel %d,%d: %d, want %d", x, y, got, want)
				}
			}
		}
	}
}
