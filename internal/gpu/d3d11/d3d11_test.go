//go:build windows

package d3d11

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/egoist/mygo/internal/gpu"
	"github.com/egoist/mygo/internal/gpu/gputest"
)

// hiddenWindow creates a window that never shows, for a swap chain.
func hiddenWindow(t *testing.T, w, h int) uintptr {
	user32 := syscall.NewLazyDLL("user32.dll")
	class, _ := syscall.UTF16PtrFromString("STATIC")
	const wsPopup = 0x80000000
	hwnd, _, err := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), 0, wsPopup, 0, 0, uintptr(w), uintptr(h), 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatal(err)
	}
	t.Cleanup(func() { user32.NewProc("DestroyWindow").Call(hwnd) })
	return hwnd
}

// readBack copies the back buffer to memory: BGRA rows and their stride.
func (r *Renderer) readBack(t *testing.T) ([]byte, int) {
	var back, staging uintptr
	if hr := call(r.swapChain, scGetBuffer, 0, uintptr(unsafe.Pointer(&iidID3D11Texture2D)), uintptr(unsafe.Pointer(&back))); failed(hr) {
		t.Fatalf("no back buffer: %#x", uint32(hr))
	}
	defer free(&back)
	const usageStaging, cpuAccessRead, mapRead, ctxCopyResource = 3, 0x20000, 1, 47
	desc := texture2DDesc{Width: uint32(r.w), Height: uint32(r.h), MipLevels: 1, ArraySize: 1, Format: formatB8G8R8A8Unorm,
		SampleCount: 1, Usage: usageStaging, CPUAccessFlags: cpuAccessRead}
	if hr := call(r.device, devCreateTexture2D, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&staging))); failed(hr) {
		t.Fatalf("no staging texture: %#x", uint32(hr))
	}
	defer free(&staging)
	call(r.ctx, ctxCopyResource, staging, back)
	var m mapped
	if hr := call(r.ctx, ctxMap, staging, 0, mapRead, 0, uintptr(unsafe.Pointer(&m))); failed(hr) {
		t.Fatalf("cannot map: %#x", uint32(hr))
	}
	defer call(r.ctx, ctxUnmap, staging, 0)
	pix := make([]byte, r.w*r.h*4)
	for y := range r.h {
		copy(pix[y*r.w*4:], unsafe.Slice((*byte)(ptr(m.Data+uintptr(y)*uintptr(m.RowPitch))), r.w*4))
	}
	return pix, r.w * 4
}

func TestDrawsAsTheCPURenderer(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// With the shaders compiled ahead of time, and with those compiled
	// from shader.hlsl when the renderer starts, as when they are older.
	for _, compile := range []bool{false, true} {
		compileShaders = compile
		s := gputest.Scene()
		r, err := New(hiddenWindow(t, s.Width, s.Height))
		compileShaders = false
		if err != nil {
			t.Skip("no Direct3D 11:", err)
		}
		// Twice: the second frame updates what the first uploaded.
		for frame := range 2 {
			if err := r.draw(s); err != nil {
				t.Fatal(err)
			}
			pix, stride := r.readBack(t)
			gputest.Compare(t, "d3d11", pix, stride, s)
			if err := r.present(); err != nil {
				t.Fatalf("frame %d: %v", frame, err)
			}
		}
		r.Release()
	}
}

// TestShaderBytecode checks that the shaders compiled ahead of time come
// from shader.hlsl as it is, and that the compiler renderers fall back to
// compiles it.
func TestShaderBytecode(t *testing.T) {
	if gpu.SourceSum(shaderSource) != shaderSum {
		t.Fatal("shader.hlsl changed since shaders.go was generated: run go generate ./internal/gpu/d3d11 on Windows")
	}
	for _, s := range []struct{ entry, target string }{{"vs", "vs_4_0"}, {"ps", "ps_4_0"}} {
		code, err := compileShader(s.entry, s.target)
		if err != nil {
			t.Fatal(err)
		}
		// DXBC starts with its magic.
		if len(code) < 4 || string(code[:4]) != "DXBC" {
			t.Errorf("%s: not DXBC", s.entry)
		}
	}
}
