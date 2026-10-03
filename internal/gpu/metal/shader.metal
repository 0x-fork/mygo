// The one shader of the Metal renderer, as shader.hlsl is the Direct3D
// one: every scene op is an instanced quad, and the fragment shader
// computes the coverage of rounded rectangles, borders, gradients and
// shadows from signed distances, as the CPU renderer (internal/raster)
// does. Colors are straight (not premultiplied) and blending happens in
// sRGB space, as in browsers. go generate compiles it into shaderlib.go.

#include <metal_stdlib>
using namespace metal;

// One instance per op, as internal/gpu builds them.
struct Inst {
	float4 rect;      // x, y, width, height in pixels
	float4 radii;     // top-left, top-right, bottom-right, bottom-left
	float4 inner;     // radii of the border's inner edge
	float4 color;
	float4 color2;    // gradient end
	float4 border;    // border color
	float4 grad;      // gradient start and end points
	float4 uv;        // texture rectangle, normalized
	float4 clip;      // the innermost clip rectangle
	float4 clipRadii;
	float4 params;    // kind, border width, sigma or gradient flag, opacity
};

struct VSOut {
	float4 pos [[position]];
	float2 p;
	float2 tex;
	uint inst [[flat]];
};

vertex VSOut vs(uint vid [[vertex_id]], uint iid [[instance_id]],
                const device Inst *insts [[buffer(0)]],
                constant float4 &globals [[buffer(1)]]) {
	Inst i = insts[iid];
	float2 corner = float2(float(vid & 1u), float(vid >> 1u));
	float4 r = i.rect;
	float kind = i.params.x;
	if (kind < 0.5f) {
		r = float4(r.xy - 1.0f, r.zw + 2.0f);
	} else if (kind < 1.5f) {
		float e = 3.0f * i.params.z + 1.0f;
		r = float4(r.xy - e, r.zw + 2.0f * e);
	}
	float2 p = r.xy + corner * r.zw;
	VSOut o;
	o.pos = float4(p / globals.xy * float2(2.0f, -2.0f) + float2(-1.0f, 1.0f), 0.0f, 1.0f);
	o.p = p;
	o.tex = mix(i.uv.xy, i.uv.zw, corner);
	o.inst = iid;
	return o;
}

float sdRoundRect(float2 p, float4 rect, float4 radii) {
	float2 h = rect.zw * 0.5f;
	float2 q = p - rect.xy - h;
	float r = q.x < 0.0f ? (q.y < 0.0f ? radii.x : radii.w) : (q.y < 0.0f ? radii.y : radii.z);
	float2 a = abs(q) - h + r;
	return length(max(a, float2(0.0f))) + min(max(a.x, a.y), 0.0f) - r;
}

float coverage(float d) { return saturate(0.5f - d); }

float4 premul(float4 c) { return float4(c.rgb * c.a, c.a); }

// erf2 approximates the error function (Abramowitz and Stegun 7.1.27).
float2 erf2(float2 x) {
	float2 s = sign(x);
	float2 a = abs(x);
	x = 1.0f + (0.278393f + (0.230389f + 0.078108f * (a * a)) * a) * a;
	x *= x;
	return s - s / (x * x);
}

float gaussian(float x, float sigma) {
	return exp(-(x * x) / (2.0f * sigma * sigma)) / (2.50662827463f * sigma);
}

// The blurred rounded box of Evan Wallace: exact along x, four samples
// along y.
float shadowX(float x, float y, float sigma, float corner, float2 h) {
	float delta = min(h.y - corner - abs(y), 0.0f);
	float curved = h.x - corner + sqrt(max(0.0f, corner * corner - delta * delta));
	float2 integral = 0.5f + 0.5f * erf2((x + float2(-curved, curved)) * (sqrt(0.5f) / sigma));
	return integral.y - integral.x;
}

float boxShadow(float2 p, float4 rect, float sigma, float corner) {
	float2 h = rect.zw * 0.5f;
	p -= rect.xy + h;
	float low = p.y - h.y;
	float high = p.y + h.y;
	float from = clamp(-3.0f * sigma, low, high);
	float to = clamp(3.0f * sigma, low, high);
	float dy = (to - from) / 4.0f;
	float y = from + dy * 0.5f;
	float v = 0.0f;
	for (int k = 0; k < 4; k++) {
		v += shadowX(p.x, p.y - y, sigma, corner, h) * gaussian(y, sigma) * dy;
		y += dy;
	}
	return v;
}

fragment float4 ps(VSOut v [[stage_in]],
                   const device Inst *insts [[buffer(0)]],
                   texture2d<float> maskTex [[texture(0)]],
                   texture2d<float> colorTex [[texture(1)]],
                   texture2d<float> imageTex [[texture(2)]],
                   sampler samp [[sampler(0)]]) {
	Inst i = insts[v.inst];
	float kind = i.params.x;
	float4 res;
	if (kind < 0.5f) {
		float outer = coverage(sdRoundRect(v.p, i.rect, i.radii));
		float4 c = i.color;
		if (i.params.z > 0.5f) {
			float2 d = i.grad.zw - i.grad.xy;
			float t = saturate(dot(v.p - i.grad.xy, d) / max(dot(d, d), 0.0001f));
			c = mix(i.color, i.color2, t);
		}
		res = premul(c) * outer;
		float bw = i.params.y;
		if (bw > 0.0f) {
			float4 ir = float4(i.rect.xy + bw, i.rect.zw - 2.0f * bw);
			float innerCov = (ir.z > 0.0f && ir.w > 0.0f) ? coverage(sdRoundRect(v.p, ir, i.inner)) : 0.0f;
			float bc = saturate(outer - innerCov);
			float4 b = premul(i.border) * bc;
			res = b + res * (1.0f - b.a);
		}
	} else if (kind < 1.5f) {
		float corner = max(max(i.radii.x, i.radii.y), max(i.radii.z, i.radii.w));
		res = premul(i.color) * boxShadow(v.p, i.rect, i.params.z, corner);
	} else if (kind < 2.5f) {
		res = premul(i.color) * maskTex.sample(samp, v.tex).r;
	} else if (kind < 3.5f) {
		res = colorTex.sample(samp, v.tex) * i.color.a;
	} else {
		res = imageTex.sample(samp, v.tex) * coverage(sdRoundRect(v.p, i.rect, i.radii));
	}
	float clip = coverage(sdRoundRect(v.p, i.clip, i.clipRadii));
	return res * clip * i.params.w;
}
