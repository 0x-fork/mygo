//go:build amd64 || arm64

package glass

import (
	"runtime"
	"testing"

	"github.com/egoist/mygo/internal/gpu/gl"
	"github.com/egoist/mygo/internal/gpu/gputest"
)

// TestOpenGL checks that OpenGL 3.3 and OpenGL ES 3.0 draw the glass as
// the CPU does.
func TestOpenGL(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, es := range []bool{false, true} {
		s := testScene()
		o, err := gl.NewOffscreen(es, s.Width, s.Height)
		if err != nil {
			t.Skip(err)
		}
		t.Log(o.Info())
		pix, err := o.Render(s)
		o.Release()
		if err != nil {
			t.Fatal(err)
		}
		gputest.Compare(t, "glass-gl", pix, s.Width*4, s)
	}
}
