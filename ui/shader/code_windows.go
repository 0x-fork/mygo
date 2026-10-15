package shader

import (
	"github.com/egoist/mygo/internal/scene"
	"github.com/egoist/mygo/internal/shaderir"
	"github.com/egoist/mygo/internal/shaderir/hlsl"
)

// code gives fx p's code for the Direct3D renderer, as an effect's, with
// the uniforms in the cbuffer of register b2 and the images in registers
// t6 to t9, after the renderer's own, and returns where the uniforms'
// lanes lie in the cbuffer and its size.
func code(p *shaderir.Program, fx *scene.Effect) ([]int, int) {
	fx.HLSL.Source = hlsl.Fragment(p, "shaderFragment", 2, 6) + `
float4 effect(float2 p, Effect e) {
	return shaderFragment(float4(p, 0, 1), (p - e.rect.xy) / U2);
}
`
	return hlsl.UniformLanes(p)
}
