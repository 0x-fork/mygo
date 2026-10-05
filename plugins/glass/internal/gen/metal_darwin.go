package main

import (
	"os"

	"github.com/egoist/mygo/internal/gpu/metal"
)

// compile compiles glass.metal within the Metal renderer's head and tail.
func compile() (name, compiled string, code []byte, err error) {
	src, err := os.ReadFile("glass.metal")
	if err != nil {
		return "", "", nil, err
	}
	compiled = metal.EffectSource(string(src))
	code, err = metal.CompileLibrary(compiled)
	return "glass.metal", compiled, code, err
}
