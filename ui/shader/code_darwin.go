package shader

import (
	"github.com/egoist/mygo/internal/gpu/metal"
	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/shaderir"
	"github.com/egoist/mygo/internal/shaderir/msl"
)

// code gives fx p's code for the Metal renderer, and returns where the
// uniforms' lanes lie in its buffer and the buffer's size: the whole
// library, whose tail (ShaderSource in package metal) calls shaderMain.
func code(p *shaderir.Program, fx *scene.Effect) ([]int, int) {
	fx.Metal.Source = metal.ShaderSource(msl.Fragment(p, "shaderFragment") + `
float4 shaderMain(constant Uniforms& uniforms, texture2d<float> T0, texture2d<float> T1, texture2d<float> T2, texture2d<float> T3, float2 p, float4 rect) {
	return shaderFragment(uniforms, T0, T1, T2, T3, float4(p, 0.0f, 1.0f), (p - rect.xy) / uniforms.U2);
}
`)
	return msl.UniformLanes(p)
}
