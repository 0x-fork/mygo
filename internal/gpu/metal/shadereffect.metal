// The tail of a shader's library (package ui/shader), after the head of
// effect.metal and the shader's code, which ends with shaderMain: Metal
// has no global variables, so a shader's uniforms (buffer 2) and images
// (textures 5 to 8) come to the fragment function, which passes them on.

fragment PSOut shader_ps(VSOut v [[stage_in]], const device Inst *insts [[buffer(0)]],
						 constant float4 &globals [[buffer(1)]], constant Uniforms &uniforms [[buffer(2)]],
						 texture2d<float> T0 [[texture(5)]], texture2d<float> T1 [[texture(6)]],
						 texture2d<float> T2 [[texture(7)]], texture2d<float> T3 [[texture(8)]],
						 texture2d<float> clipTex [[texture(4)]], sampler samp [[sampler(0)]]) {
	Inst i = insts[v.inst];
	Frame fr = frameOf(v, i);
	float4 res = shaderMain(uniforms, T0, T1, T2, T3, v.p, i.rect) *
				 (localCoverage(v.p, i.rect, i.radii, i.transform0.w, fr) * clipCoverage(v, i, clipTex, samp) * i.params.w);
	return PSOut{res, res.aaaa};
}
