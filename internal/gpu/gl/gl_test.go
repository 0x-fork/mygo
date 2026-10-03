//go:build linux && (amd64 || arm64)

package gl

import (
	"bytes"
	"runtime"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/gpu/gputest"
	"github.com/egoist/mygo/internal/raster"
)

// EGL enumerations.
const (
	eglPlatformSurfaceless   = 0x31DD // EGL_PLATFORM_SURFACELESS_MESA
	eglNone                  = 0x3038
	eglRenderableType        = 0x3040
	eglOpenGLBit             = 0x0008
	eglSurfaceType           = 0x3033
	eglPbufferBit            = 0x0001
	eglOpenGLAPI             = 0x30A2
	eglOpenGLESAPI           = 0x30A0
	eglOpenGLES3Bit          = 0x0040 // EGL_OPENGL_ES3_BIT_KHR
	eglContextMajorVersion   = 0x3098
	eglContextMinorVersion   = 0x30FB
	eglContextProfileMask    = 0x30FD
	eglContextCoreProfileBit = 0x1
)

// apis are the APIs the renderer draws with: OpenGL 3.3 and OpenGL ES 3.0.
var apis = []struct {
	name string
	es   bool
}{{"OpenGL", false}, {"OpenGL ES", true}}

// offscreen makes an OpenGL 3.3 context, or with es an OpenGL ES 3.0 one,
// current on the calling thread, without a window, with Mesa's surfaceless
// EGL platform, and binds a framebuffer of w×h pixels. read returns its
// pixels as the CPU renderer draws them: premultiplied BGRA rows from the
// top.
func offscreen(t *testing.T, es bool, w, h int) (read func() []byte) {
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	lib, err := purego.Dlopen("libEGL.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		t.Skip("no EGL:", err)
	}
	var (
		getPlatformDisplay func(platform uint32, native uintptr, attribs unsafe.Pointer) uintptr
		initialize         func(display uintptr, major, minor *int32) uint32
		bindAPI            func(api uint32) uint32
		chooseConfig       func(display uintptr, attribs *int32, configs *uintptr, size int32, n *int32) uint32
		createContext      func(display, config, share uintptr, attribs *int32) uintptr
		makeCurrent        func(display, draw, read, context uintptr) uint32
		destroyContext     func(display, context uintptr) uint32
		terminate          func(display uintptr) uint32
	)
	for name, fn := range map[string]any{
		"eglGetPlatformDisplay": &getPlatformDisplay, "eglInitialize": &initialize, "eglBindAPI": &bindAPI,
		"eglChooseConfig": &chooseConfig, "eglCreateContext": &createContext, "eglMakeCurrent": &makeCurrent,
		"eglDestroyContext": &destroyContext, "eglTerminate": &terminate,
	} {
		sym, err := purego.Dlsym(lib, name)
		if err != nil || sym == 0 {
			t.Skip("EGL has no", name)
		}
		purego.RegisterFunc(fn, sym)
	}
	display := getPlatformDisplay(eglPlatformSurfaceless, 0, nil)
	if display == 0 || initialize(display, nil, nil) == 0 {
		t.Skip("no surfaceless EGL display")
	}
	t.Cleanup(func() { terminate(display) })
	api, bit, contextAttribs := uint32(eglOpenGLAPI), int32(eglOpenGLBit), []int32{eglContextMajorVersion, 3, eglContextMinorVersion, 3, eglContextProfileMask, eglContextCoreProfileBit, eglNone}
	if es {
		api, bit, contextAttribs = eglOpenGLESAPI, eglOpenGLES3Bit, []int32{eglContextMajorVersion, 3, eglContextMinorVersion, 0, eglNone}
	}
	bindAPI(api)
	// Configurations are for windows unless asked otherwise.
	configAttribs := []int32{eglRenderableType, bit, eglSurfaceType, eglPbufferBit, eglNone}
	var config uintptr
	var n int32
	if chooseConfig(display, &configAttribs[0], &config, 1, &n) == 0 || n == 0 {
		t.Skip("no EGL configuration for the API")
	}
	context := createContext(display, config, 0, &contextAttribs[0])
	if context == 0 {
		t.Skip("no context of the version")
	}
	t.Cleanup(func() {
		makeCurrent(display, 0, 0, 0)
		destroyContext(display, context)
	})
	if makeCurrent(display, 0, 0, context) == 0 {
		t.Skip("cannot make the context current without a surface")
	}
	if err := load(); err != nil {
		t.Skip(err)
	}
	t.Logf("%s, %s", goString(glGetString(glVersion)), goString(glGetString(glRenderer)))

	tex := newTexture(glRGBA8, glRGBA, w, h, nil, 0)
	var fb uint32
	glGenFramebuffers(1, &fb)
	glBindFramebuffer(glFramebuffer, fb)
	glFramebufferTexture2D(glFramebuffer, glColorAttachment0, glTexture2D, tex, 0)
	if status := glCheckFramebufferState(glFramebuffer); status != glFramebufferDone {
		t.Fatalf("framebuffer incomplete: %#x", status)
	}
	t.Cleanup(func() {
		glBindFramebuffer(glFramebuffer, 0)
		glDeleteFramebuffers(1, &fb)
		glDeleteTextures(1, &tex)
	})
	return func() []byte {
		pix, err := ReadFramebuffer(w, h)
		if err != nil {
			t.Fatal(err)
		}
		return pix
	}
}

func TestDrawsAsTheCPURenderer(t *testing.T) {
	for _, api := range apis {
		t.Run(api.name, func(t *testing.T) {
			s := gputest.Scene()
			read := offscreen(t, api.es, s.Width, s.Height)
			r, err := New()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Release()
			if r.es != api.es {
				t.Fatalf("the renderer takes the context for OpenGL ES: %v", r.es)
			}
			// Twice: the second frame updates what the first uploaded.
			for range 2 {
				if err := r.Render(s); err != nil {
					t.Fatal(err)
				}
				gputest.Compare(t, "gl", read(), s.Width*4, s)
			}
		})
	}
}

func TestPresentsFramesDrawnInMemory(t *testing.T) {
	for _, api := range apis {
		t.Run(api.name, func(t *testing.T) {
			s := gputest.Scene()
			read := offscreen(t, api.es, s.Width, s.Height)
			want := raster.NewImage(s.Width, s.Height)
			raster.Render(want, s)
			var p Presenter
			// Twice: the second frame reuses the texture.
			for range 2 {
				if err := p.Present(want.Pix, want.Stride, want.W, want.H); err != nil {
					t.Fatal(err)
				}
				if got := read(); !bytes.Equal(got, want.Pix[:len(got)]) {
					t.Fatal("the frame presented differs from the frame drawn")
				}
			}
		})
	}
}
