package shader_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/shader"
	"github.com/egoist/mygo/internal/shaderir/glsl"
	"github.com/egoist/mygo/internal/shaderir/hlsl"
	"github.com/egoist/mygo/internal/shaderir/msl"
)

// The backends write a shader as a function for the renderers to call,
// as the files beside testdata/mygo/fragment.go hold, which MYGO_UPDATE_SHADERS
// writes anew.
func TestFragment(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "mygo", "fragment.go"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := shader.CompileFragment(src)
	if err != nil {
		t.Fatal(err)
	}
	for ext, got := range map[string]string{
		"metal": msl.Fragment(p, "shaderFragment"),
		"glsl":  glsl.Fragment(p, "shaderFragment", "flipY"),
		"hlsl":  hlsl.Fragment(p, "shaderFragment", 2, 6),
	} {
		name := filepath.Join("testdata", "mygo", "fragment.expected."+ext)
		if os.Getenv("MYGO_UPDATE_SHADERS") != "" {
			if err := os.WriteFile(name, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%s: got\n%s", name, got)
		}
		// A uint loop steps by a uint, and HLSL makes a vector of one scalar
		// by a cast, as it does not spread one.
		if !strings.Contains(got, "+= 2u") {
			t.Errorf("%s: the uint loop does not step by 2u", name)
		}
		if ext == "hlsl" && !strings.Contains(got, "(uint3)(") {
			t.Errorf("%s: uvec3 of a scalar is not a cast", name)
		}
	}
}

// Shaders read uints and ints as two kinds of integer, which mix only in
// shifts, and constants must fit theirs.
func TestUint(t *testing.T) {
	for _, c := range []struct {
		body, err string
	}{
		{"var x uint = 0xFFFFFFFF; _ = x", ""},
		{"var a uint; x := a * 2654435769; _ = x", ""},
		{"x := uint(3) * 2654435769; _ = x", "overflows uint"},
		{"var x uint = 1 << 32; _ = x", "overflows uint"},
		{"x := uint(-1); _ = x", "cannot convert"},
		{"var a int; var b uint; _ = a + b", "types don't match"},
		{"var a uint; _ = a << 3; _ = a >> int(dst.x); _ = 1 << a", ""},
		{"var v uvec2; v.x ^= 7; v %= 3; _ = v.yx + uvec2(dst.xy)", ""},
		{"x := int(2654435769); _ = x", "overflows int"},
		{"_ = max(uint(1), 2, 3); _ = clamp(uvec2(1), uint(0), 4)", ""},
	} {
		src := "package main\n\nfunc Fragment(dst vec4, pos vec2) vec4 {\n\t" + c.body + "\n\treturn dst\n}\n"
		_, err := shader.CompileFragment([]byte(src))
		switch {
		case c.err == "" && err != nil:
			t.Errorf("%s: %v", c.body, err)
		case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
			t.Errorf("%s: %v, want an error saying %q", c.body, err, c.err)
		}
	}
}

// A shader needs its entry point.
func TestNoEntry(t *testing.T) {
	if _, err := shader.CompileFragment([]byte("package main\n\nfunc main() {}\n")); err == nil {
		t.Error("a shader without Fragment compiled")
	}
}
