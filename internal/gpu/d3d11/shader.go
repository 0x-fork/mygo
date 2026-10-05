//go:build windows

package d3d11

import (
	_ "embed"
	"fmt"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo/internal/gpu"
	"github.com/egoist/mygo/internal/gpu/d3d11/device"
)

//go:embed shader.hlsl
var shaderSource string

// compileShaders makes shaderCode compile shader.hlsl even when the
// bytecode of shaders.go comes from it, for tests.
var compileShaders bool

// shaderCode returns the bytecode of the vertex and pixel shaders: that
// compiled ahead of time into shaders.go, or, when shader.hlsl changed
// since (go generate ./internal/gpu/d3d11 was not run), that compiled now
// from shader.hlsl, with the compiler Windows has.
func shaderCode() (vs, ps []byte, err error) {
	if gpu.SourceSum(shaderSource) == shaderSum && !compileShaders {
		return vertexShader, pixelShader, nil
	}
	if vs, err = compileShader("vs", "vs_4_0"); err != nil {
		return nil, nil, err
	}
	if ps, err = compileShader("ps", "ps_4_0"); err != nil {
		return nil, nil, err
	}
	return vs, ps, nil
}

// procD3DCompile is the shader compiler of Windows 10 and later, which gen.go
// compiles shaders.go with too.
var procD3DCompile = syscall.NewLazyDLL(device.SystemDir() + `\d3dcompiler_47.dll`).NewProc("D3DCompile")

// compileShader compiles the function entry of shader.hlsl for target, as
// gen.go does.
func compileShader(entry, target string) ([]byte, error) {
	if err := procD3DCompile.Find(); err != nil {
		return nil, fmt.Errorf("d3d11: no shader compiler: %w", err)
	}
	src := []byte(shaderSource)
	name, _ := syscall.BytePtrFromString("shader.hlsl")
	e, _ := syscall.BytePtrFromString(entry)
	t, _ := syscall.BytePtrFromString(target)
	const optimize3 = 1 << 15
	var blob, errs uintptr
	hr, _, _ := procD3DCompile.Call(uintptr(unsafe.Pointer(&src[0])), uintptr(len(src)), uintptr(unsafe.Pointer(name)),
		0, 0, uintptr(unsafe.Pointer(e)), uintptr(unsafe.Pointer(t)), optimize3, 0,
		uintptr(unsafe.Pointer(&blob)), uintptr(unsafe.Pointer(&errs)))
	if errs != 0 {
		defer free(&errs)
	}
	if failed(hr) || blob == 0 {
		msg := "unknown error"
		if errs != 0 {
			msg = string(blobBytes(errs))
		}
		return nil, fmt.Errorf("d3d11: cannot compile the %s shader: %s", entry, msg)
	}
	defer free(&blob)
	return append([]byte(nil), blobBytes(blob)...), nil
}

// blobBytes returns the contents of an ID3DBlob, until it is released.
func blobBytes(blob uintptr) []byte {
	const blobGetBufferPointer, blobGetBufferSize = 3, 4
	p, n := call(blob, blobGetBufferPointer), call(blob, blobGetBufferSize)
	return unsafe.Slice((*byte)(ptr(p)), n)
}
