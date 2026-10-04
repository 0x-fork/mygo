// The one shader of the Metal renderer, as shader.hlsl is the Direct3D
// one: every scene op is an instanced quad, and the fragment shader
// computes the coverage of rounded rectangles, borders, gradients, stripes
// and shadows from signed distances, as the CPU renderer (internal/raster)
// does. Colors are straight (not premultiplied) and blending happens in
// sRGB space, as in browsers, with a second color for the source's alpha
// of each channel, which subpixel glyphs need. go generate compiles it
// into shaderlib.go.

#include <metal_stdlib>
using namespace metal;

// One instance per op, as internal/gpu builds them.
struct Inst {
	float4 rect;      // x, y, width, height in pixels
	float4 radii;     // top-left, top-right, bottom-right, bottom-left
	float4 inner;     // radii of the border's inner edge, or of the box casting a shadow
	float4 color;
	float4 color2;    // gradient end
	float4 border;    // border color
	float4 grad;      // gradient start and end points, or stripes
	float4 uv;        // texture rectangle, normalized, border widths, or the box casting a shadow
	float4 clip;      // the innermost clip rectangle
	float4 clipRadii;
	float4 params;    // kind, dashed or grayscale, sigma or paint, opacity
};

// The color and, for dual-source blending, the source's alpha of each
// channel.
struct PSOut {
	float4 color [[color(0), index(0)]];
	float4 alpha [[color(0), index(1)]];
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

// rectCoverage returns how much of the pixel at p a rounded rectangle
// covers: by the distance to its edge near rounded corners, and exactly,
// the area of the pixel inside it, near square ones.
float rectCoverage(float2 p, float4 rect, float4 radii) {
	float2 q = p - rect.xy - rect.zw * 0.5f;
	float r = q.x < 0.0f ? (q.y < 0.0f ? radii.x : radii.w) : (q.y < 0.0f ? radii.y : radii.z);
	if (r > 0.0f) {
		return coverage(sdRoundRect(p, rect, radii));
	}
	float2 c = saturate(min(rect.xy + rect.zw, p + 0.5f) - max(rect.xy, p - 0.5f));
	return c.x * c.y;
}

float4 premul(float4 c) { return float4(c.rgb * c.a, c.a); }

float3 unpremul(float4 c) { return c.a > 0.0f ? c.rgb / c.a : float3(0.0f); }

// textCoverage corrects the coverage a of a glyph of straight color c as
// Direct2D blends text (scene.TextCoverage): it enhances the contrast,
// the more the darker c is, and corrects for gamma with the ratios g.
float textCoverage(float a, float3 c, float contrast, float boost, float4 g) {
	float k = contrast * saturate(3.0f - 4.0f * dot(c, float3(0.30f, 0.59f, 0.11f))) + boost;
	a = a * (k + 1.0f) / (a * k + 1.0f);
	float f = dot(c, float3(0.25f, 0.5f, 0.25f));
	return saturate(a + a * (1.0f - a) * ((g.x * f + g.y) * a + (g.z * f + g.w)));
}

// subpixelCoverage does the same for each subpixel of a subpixel glyph.
float3 subpixelCoverage(float3 a, float3 c, float contrast, float boost, float4 g) {
	float k = contrast * saturate(3.0f - 4.0f * dot(c, float3(0.30f, 0.59f, 0.11f))) + boost;
	a = a * (k + 1.0f) / (a * k + 1.0f);
	return saturate(a + a * (1.0f - a) * ((g.x * c + g.y) * a + (g.z * c + g.w)));
}

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

float3 toLinear(float3 c) {
	return select(pow((c + 0.055f) / 1.055f, float3(2.4f)), c / 12.92f, c <= 0.04045f);
}

float3 toSRGB(float3 c) {
	return select(1.055f * pow(max(c, float3(0.0f)), float3(1.0f / 2.4f)) - 0.055f, c * 12.92f, c <= 0.0031308f);
}

float3 cbrt3(float3 v) { return sign(v) * pow(abs(v), float3(1.0f / 3.0f)); }

float3 oklab(float3 srgb) {
	float3 c = toLinear(srgb);
	float3 lms = cbrt3(float3(
		0.4122214708f * c.r + 0.5363325363f * c.g + 0.0514459929f * c.b,
		0.2119034982f * c.r + 0.6806995451f * c.g + 0.1073969566f * c.b,
		0.0883024619f * c.r + 0.2817188376f * c.g + 0.6299787005f * c.b));
	return float3(
		0.2104542553f * lms.x + 0.7936177850f * lms.y - 0.0040720468f * lms.z,
		1.9779984951f * lms.x - 2.4285922050f * lms.y + 0.4505937099f * lms.z,
		0.0259040371f * lms.x + 0.7827717662f * lms.y - 0.8086757660f * lms.z);
}

float3 fromOklab(float3 lab) {
	float3 lms = float3(
		lab.x + 0.3963377774f * lab.y + 0.2158037573f * lab.z,
		lab.x - 0.1055613458f * lab.y - 0.0638541728f * lab.z,
		lab.x - 0.0894841775f * lab.y - 1.2914855480f * lab.z);
	lms = lms * lms * lms;
	return toSRGB(float3(
		4.0767416621f * lms.x - 3.3077115913f * lms.y + 0.2309699292f * lms.z,
		-1.2684380046f * lms.x + 2.6097574011f * lms.y - 0.3413193965f * lms.z,
		-0.0041960863f * lms.x - 0.7034186147f * lms.y + 1.7076147010f * lms.z));
}

// paint returns the premultiplied color at p of plain color, a gradient
// mixed in sRGB (1) or Oklab (2), or stripes (3), as scene.Paint says.
float4 paint(float2 p, float mode, float4 rect, float4 c1, float4 c2, float4 g) {
	if (mode < 0.5f) {
		return premul(c1);
	}
	if (mode < 2.5f) {
		float2 d = g.zw - g.xy;
		float t = saturate(dot(p - g.xy, d) / max(dot(d, d), 0.0001f));
		float a = mix(c1.a, c2.a, t);
		if (mode < 1.5f) {
			return float4(mix(c1.rgb * c1.a, c2.rgb * c2.a, t), a);
		}
		float3 lab = mix(oklab(c1.rgb) * c1.a, oklab(c2.rgb) * c2.a, t);
		return a > 0.0f ? float4(saturate(fromOklab(lab / a)) * a, a) : float4(0.0f);
	}
	float s = dot(p - rect.xy, g.xy);
	float phase = s - g.w * floor(s / g.w);
	float cov = coverage(min(max(-phase, phase - g.z), g.w - phase));
	return premul(c1) * cov + premul(c2) * (1.0f - cov);
}

// dash returns how much of a dashed border shows at p: each side, which
// the pixel belongs to when it is nearest that side's edge in widths of
// its border, has an odd number of dashes and gaps of equal length, about
// three widths, starting and ending with a dash.
float dash(float2 p, float4 rect, float4 w) {
	float2 q = p - rect.xy;
	float dt = w.x > 0.0f ? q.y / w.x : 1e9f;
	float dr = w.y > 0.0f ? (rect.z - q.x) / w.y : 1e9f;
	float db = w.z > 0.0f ? (rect.w - q.y) / w.z : 1e9f;
	float dl = w.w > 0.0f ? q.x / w.w : 1e9f;
	float s, len, bw;
	if (dt <= dr && dt <= db && dt <= dl) {
		s = q.x; len = rect.z; bw = w.x;
	} else if (dr <= db && dr <= dl) {
		s = q.y; len = rect.w; bw = w.y;
	} else if (db <= dl) {
		s = rect.z - q.x; len = rect.z; bw = w.z;
	} else {
		s = rect.w - q.y; len = rect.w; bw = w.w;
	}
	// n - 1 is how many periods of a dash and a gap, six widths, fit in
	// len: GPUs divide less exactly than CPUs, and may count one too few
	// or too many where len is a whole number of periods, as sides often
	// are, so the count is checked by multiplying back.
	float n = max(1.0f, floor((len / (3.0f * bw) + 1.0f) * 0.5f + 0.5f));
	float period = 6.0f * bw;
	n += n * period <= len ? 1.0f : 0.0f;
	n -= (n - 1.0f) * period > len ? 1.0f : 0.0f;
	float seg = len / (2.0f * n - 1.0f);
	float k = floor(s / seg);
	float f = s - k * seg;
	float edge = min(f, seg - f);
	return coverage(k - 2.0f * floor(k * 0.5f) < 0.5f ? -edge : edge);
}

fragment PSOut ps(VSOut v [[stage_in]],
                   const device Inst *insts [[buffer(0)]],
                   texture2d<float> maskTex [[texture(0)]],
                   texture2d<float> colorTex [[texture(1)]],
                   texture2d<float> imageTex [[texture(2)]],
                   sampler samp [[sampler(0)]]) {
	Inst i = insts[v.inst];
	float kind = i.params.x;
	float4 res;
	if (kind < 0.5f) {
		float outer = rectCoverage(v.p, i.rect, i.radii);
		res = paint(v.p, i.params.z, i.rect, i.color, i.color2, i.grad) * outer;
		float4 bw = i.uv; // top, right, bottom, left
		if (any(bw > 0.0f)) {
			float4 ir = float4(i.rect.xy + bw.wx, i.rect.zw - bw.yz - bw.wx);
			float innerCov = (ir.z > 0.0f && ir.w > 0.0f) ? rectCoverage(v.p, ir, i.inner) : 0.0f;
			float bc = saturate(outer - innerCov);
			if (i.params.y > 0.5f) {
				bc *= dash(v.p, i.rect, bw);
			}
			float4 b = premul(i.border) * bc;
			res = b + res * (1.0f - b.a);
		}
	} else if (kind < 1.5f) {
		float sigma = i.params.z;
		float corner = max(max(i.radii.x, i.radii.y), max(i.radii.z, i.radii.w));
		float s = sigma > 0.0f ? boxShadow(v.p, i.rect, sigma, corner) : rectCoverage(v.p, i.rect, i.radii);
		if (i.uv.z > 0.0f && i.uv.w > 0.0f) {
			s *= 1.0f - rectCoverage(v.p, i.uv, i.inner); // outside the box casting it
		}
		res = premul(i.color) * s;
	} else if (kind < 2.5f) {
		float4 c = paint(v.p, i.params.z, i.rect, i.color, i.color2, i.grad);
		res = c * textCoverage(maskTex.sample(samp, v.tex).r, unpremul(c), i.inner.x, i.inner.y, i.radii);
	} else if (kind < 3.5f) {
		res = colorTex.sample(samp, v.tex) * i.color.a;
	} else if (kind < 4.5f) {
		res = imageTex.sample(samp, v.tex) * rectCoverage(v.p, i.rect, i.radii);
		if (i.params.y > 0.5f) {
			res.rgb = float3(dot(res.rgb, float3(0.2126f, 0.7152f, 0.0722f)));
		}
	} else {
		float4 c = paint(v.p, i.params.z, i.rect, i.color, i.color2, i.grad);
		float3 straight = unpremul(c);
		float3 a = subpixelCoverage(colorTex.sample(samp, v.tex).rgb, straight, i.inner.x, i.inner.y, i.radii);
		float3 w = a * c.a * rectCoverage(v.p, i.clip, i.clipRadii) * i.params.w;
		float wa = (w.r + w.g + w.b) / 3.0f;
		return PSOut{float4(straight * w, wa), float4(w, wa)};
	}
	float clip = rectCoverage(v.p, i.clip, i.clipRadii);
	res *= clip * i.params.w;
	return PSOut{res, res.aaaa};
}
