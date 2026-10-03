// The one shader of the Direct3D 11 renderer: every scene op is an
// instanced quad, and the pixel shader computes the coverage of rounded
// rectangles, borders, gradients and shadows from signed distances, as the
// software renderer (internal/raster) does. Colors are straight (not
// premultiplied) and blending happens in sRGB space, as in browsers.
//
// `go generate` compiles it to DXBC (shaders.go) on Windows.

cbuffer Globals : register(b0) {
	float2 viewport;
	float2 pad;
};

struct Inst {
	float4 rect : RECT;         // x, y, width, height in pixels
	float4 radii : RADII;       // top-left, top-right, bottom-right, bottom-left
	float4 inner : INNER;       // radii of the border's inner edge
	float4 color : COLOR0;
	float4 color2 : COLOR1;     // gradient end
	float4 border : COLOR2;     // border color
	float4 grad : GRAD;         // gradient start and end points
	float4 uv : UV;             // texture rectangle, normalized
	float4 clip : CLIP;         // the innermost clip rectangle
	float4 clipRadii : CLIPR;
	float4 params : PARAMS;     // kind, border width, sigma or gradient flag, opacity
};

struct VSOut {
	float4 pos : SV_Position;
	float2 p : PIXEL;
	float2 tex : TEXCOORD0;
	nointerpolation float4 rect : RECT;
	nointerpolation float4 radii : RADII;
	nointerpolation float4 inner : INNER;
	nointerpolation float4 color : COLOR0;
	nointerpolation float4 color2 : COLOR1;
	nointerpolation float4 border : COLOR2;
	nointerpolation float4 grad : GRAD;
	nointerpolation float4 clip : CLIP;
	nointerpolation float4 clipRadii : CLIPR;
	nointerpolation float4 params : PARAMS;
};

VSOut vs(uint vid : SV_VertexID, Inst i) {
	float2 corner = float2(vid & 1, vid >> 1);
	float4 r = i.rect;
	float kind = i.params.x;
	if (kind < 0.5) {
		r = float4(r.xy - 1, r.zw + 2);
	} else if (kind < 1.5) {
		float e = 3 * i.params.z + 1;
		r = float4(r.xy - e, r.zw + 2 * e);
	}
	float2 p = r.xy + corner * r.zw;
	VSOut o;
	o.pos = float4(p / viewport * float2(2, -2) + float2(-1, 1), 0, 1);
	o.p = p;
	o.tex = lerp(i.uv.xy, i.uv.zw, corner);
	o.rect = i.rect;
	o.radii = i.radii;
	o.inner = i.inner;
	o.color = i.color;
	o.color2 = i.color2;
	o.border = i.border;
	o.grad = i.grad;
	o.clip = i.clip;
	o.clipRadii = i.clipRadii;
	o.params = i.params;
	return o;
}

Texture2D maskTex : register(t0);
Texture2D colorTex : register(t1);
Texture2D imageTex : register(t2);
SamplerState samp : register(s0);

float sdRoundRect(float2 p, float4 rect, float4 radii) {
	float2 h = rect.zw * 0.5;
	float2 q = p - rect.xy - h;
	float r = q.x < 0 ? (q.y < 0 ? radii.x : radii.w) : (q.y < 0 ? radii.y : radii.z);
	float2 a = abs(q) - h + r;
	return length(max(a, 0)) + min(max(a.x, a.y), 0) - r;
}

float coverage(float d) { return saturate(0.5 - d); }

float4 premul(float4 c) { return float4(c.rgb * c.a, c.a); }

float2 erf2(float2 x) {
	float2 s = sign(x), a = abs(x);
	x = 1 + (0.278393 + (0.230389 + 0.078108 * (a * a)) * a) * a;
	x *= x;
	return s - s / (x * x);
}

float gaussian(float x, float sigma) {
	return exp(-(x * x) / (2 * sigma * sigma)) / (2.50662827463 * sigma);
}

// The blurred rounded box of Evan Wallace: exact along x, four samples
// along y.
float shadowX(float x, float y, float sigma, float corner, float2 h) {
	float delta = min(h.y - corner - abs(y), 0);
	float curved = h.x - corner + sqrt(max(0, corner * corner - delta * delta));
	float2 integral = 0.5 + 0.5 * erf2((x + float2(-curved, curved)) * (sqrt(0.5) / sigma));
	return integral.y - integral.x;
}

float boxShadow(float2 p, float4 rect, float sigma, float corner) {
	float2 h = rect.zw * 0.5;
	p -= rect.xy + h;
	float low = p.y - h.y, high = p.y + h.y;
	float start = clamp(-3 * sigma, low, high);
	float end = clamp(3 * sigma, low, high);
	float step = (end - start) / 4;
	float y = start + step * 0.5;
	float v = 0;
	for (int k = 0; k < 4; k++) {
		v += shadowX(p.x, p.y - y, sigma, corner, h) * gaussian(y, sigma) * step;
		y += step;
	}
	return v;
}

float4 ps(VSOut i) : SV_Target {
	float kind = i.params.x;
	float4 res;
	if (kind < 0.5) {
		float outer = coverage(sdRoundRect(i.p, i.rect, i.radii));
		float4 c = i.color;
		if (i.params.z > 0.5) {
			float2 d = i.grad.zw - i.grad.xy;
			float t = saturate(dot(i.p - i.grad.xy, d) / max(dot(d, d), 0.0001));
			c = lerp(i.color, i.color2, t);
		}
		res = premul(c) * outer;
		float bw = i.params.y;
		if (bw > 0) {
			float4 ir = float4(i.rect.xy + bw, i.rect.zw - 2 * bw);
			float innerCov = (ir.z > 0 && ir.w > 0) ? coverage(sdRoundRect(i.p, ir, i.inner)) : 0;
			float bc = saturate(outer - innerCov);
			float4 b = premul(i.border) * bc;
			res = b + res * (1 - b.a);
		}
	} else if (kind < 1.5) {
		float corner = max(max(i.radii.x, i.radii.y), max(i.radii.z, i.radii.w));
		res = premul(i.color) * boxShadow(i.p, i.rect, i.params.z, corner);
	} else if (kind < 2.5) {
		res = premul(i.color) * maskTex.Sample(samp, i.tex).r;
	} else if (kind < 3.5) {
		res = colorTex.Sample(samp, i.tex) * i.color.a;
	} else {
		res = imageTex.Sample(samp, i.tex) * coverage(sdRoundRect(i.p, i.rect, i.radii));
	}
	float clip = coverage(sdRoundRect(i.p, i.clip, i.clipRadii));
	return res * clip * i.params.w;
}
