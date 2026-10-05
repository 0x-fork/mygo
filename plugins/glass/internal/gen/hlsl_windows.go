package main

import (
	"os"

	"github.com/egoist/mygo/internal/gpu/d3d11"
)

// compile compiles glass.hlsl within the Direct3D renderer's head and
// tail.
func compile() (name, compiled string, code []byte, err error) {
	src, err := os.ReadFile("glass.hlsl")
	if err != nil {
		return "", "", nil, err
	}
	code, err = d3d11.CompileEffect(string(src))
	return "glass.hlsl", d3d11.EffectSource(string(src)), code, err
}
