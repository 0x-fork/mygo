package ui

import (
	"image"
	"runtime"
	"testing"
	"time"
	"weak"

	"github.com/egoist/mygo/internal/scene"
)

// Removed elements must give up their resources while the window stays
// alive, even when the next frame has fewer elements and drawing ops.
func TestRemovedContentReleasesResources(t *testing.T) {
	for _, exits := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "exit"}[exits], func(t *testing.T) {
			show := true
			var bitmap weak.Pointer[Bitmap]
			var pixels weak.Pointer[scene.Image]
			tt, now := clockTester(func(c *Context) {
				Column(c).Children(func() {
					Text(c, "Keep the window alive")
					if !show {
						return
					}
					e := Box(c).Key("photo")
					if exits {
						e.Transition(ElementTransition{Exit: &Motion{}, Duration: 100 * time.Millisecond})
					}
					b := *Local(e, "bitmap", func() *Bitmap {
						return NewBitmap(image.NewRGBA(image.Rect(0, 0, 64, 64)))
					})
					bitmap, pixels = weak.Make(b), weak.Make(b.img)
					e.Children(func() { Image(c, b).Size(64, 64) })
				})
			}, 200, 150)
			show = false
			tt.Frame()
			if exits {
				runtime.GC()
				if bitmap.Value() == nil || pixels.Value() == nil {
					t.Fatal("the exit transition lost the image before it finished")
				}
				*now = now.Add(150 * time.Millisecond)
				tt.Frame()
			}
			runtime.GC()
			if bitmap.Value() != nil || pixels.Value() != nil {
				t.Fatal("the removed view still retains its bitmap or pixels")
			}
			runtime.KeepAlive(tt)
		})
	}
}

// A short-lived large view must not leave its entire element arena in a
// small window. A few rows may leave and enter without fresh allocations.
func TestElementArenaFollowsViewSize(t *testing.T) {
	rows := 1000
	tt := NewTester(func(c *Context) {
		for i := range rows {
			Box(c).Key(i).Height(1)
		}
	}, 100, 100)
	large := len(tt.rt.c.chunks)
	rows = 1
	tt.Frame()
	if n := len(tt.rt.c.chunks); n > 2 || n >= large {
		t.Fatalf("the small view kept %d chunks of the large view's %d", n, large)
	}
	if n := testing.AllocsPerRun(20, tt.Frame); n > 2 {
		t.Fatalf("the steady view allocates %.0f times per frame", n)
	}
}
