// The one shader of the OpenGL renderer, as shader.metal is the Metal one
// and shader.hlsl the Direct3D one: every scene op is an instanced quad,
// and the fragment shader computes the coverage of rounded rectangles,
// borders, gradients and shadows from signed distances, as the CPU
// renderer (internal/raster) does. Colors are straight (not premultiplied)
// and blending happens in sRGB space, as in browsers. The renderer
// compiles it when it starts, as GLSL 3.30 or GLSL ES 3.00, with VERTEX or
// FRAGMENT defined.

#ifdef VERTEX

// One instance per op, as internal/gpu builds them.
layout(location = 0) in vec4 aRect;      // x, y, width, height in pixels
layout(location = 1) in vec4 aRadii;     // top-left, top-right, bottom-right, bottom-left
layout(location = 2) in vec4 aInner;     // radii of the border's inner edge
layout(location = 3) in vec4 aColor;
layout(location = 4) in vec4 aColor2;    // gradient end
layout(location = 5) in vec4 aBorder;    // border color
layout(location = 6) in vec4 aGrad;      // gradient start and end points
layout(location = 7) in vec4 aUV;        // texture rectangle, normalized
layout(location = 8) in vec4 aClip;      // the innermost clip rectangle
layout(location = 9) in vec4 aClipRadii;
layout(location = 10) in vec4 aParams;   // kind, border width, sigma or gradient flag, opacity

uniform vec2 uSize;

out vec4 vPoint; // the position in pixels, and the texture coordinates
flat out vec4 vRect;
flat out vec4 vRadii;
flat out vec4 vInner;
flat out vec4 vColor;
flat out vec4 vColor2;
flat out vec4 vBorder;
flat out vec4 vGrad;
flat out vec4 vClip;
flat out vec4 vClipRadii;
flat out vec4 vParams;

void main() {
	vec2 corner = vec2(float(gl_VertexID & 1), float(gl_VertexID >> 1));
	vec4 r = aRect;
	float kind = aParams.x;
	if (kind < 0.5) {
		r = vec4(r.xy - 1.0, r.zw + 2.0);
	} else if (kind < 1.5) {
		float e = 3.0 * aParams.z + 1.0;
		r = vec4(r.xy - e, r.zw + 2.0 * e);
	}
	vec2 p = r.xy + corner * r.zw;
	gl_Position = vec4(p / uSize * vec2(2.0, -2.0) + vec2(-1.0, 1.0), 0.0, 1.0);
	vPoint = vec4(p, mix(aUV.xy, aUV.zw, corner));
	vRect = aRect;
	vRadii = aRadii;
	vInner = aInner;
	vColor = aColor;
	vColor2 = aColor2;
	vBorder = aBorder;
	vGrad = aGrad;
	vClip = aClip;
	vClipRadii = aClipRadii;
	vParams = aParams;
}

#endif

#ifdef FRAGMENT

in vec4 vPoint;
flat in vec4 vRect;
flat in vec4 vRadii;
flat in vec4 vInner;
flat in vec4 vColor;
flat in vec4 vColor2;
flat in vec4 vBorder;
flat in vec4 vGrad;
flat in vec4 vClip;
flat in vec4 vClipRadii;
flat in vec4 vParams;

uniform sampler2D uMask;
uniform sampler2D uColor;
uniform sampler2D uImage;

out vec4 fragColor;

float sdRoundRect(vec2 p, vec4 rect, vec4 radii) {
	vec2 h = rect.zw * 0.5;
	vec2 q = p - rect.xy - h;
	float r = q.x < 0.0 ? (q.y < 0.0 ? radii.x : radii.w) : (q.y < 0.0 ? radii.y : radii.z);
	vec2 a = abs(q) - h + r;
	return length(max(a, vec2(0.0))) + min(max(a.x, a.y), 0.0) - r;
}

float coverage(float d) { return clamp(0.5 - d, 0.0, 1.0); }

vec4 premul(vec4 c) { return vec4(c.rgb * c.a, c.a); }

// erf2 approximates the error function (Abramowitz and Stegun 7.1.27).
vec2 erf2(vec2 x) {
	vec2 s = sign(x);
	vec2 a = abs(x);
	x = 1.0 + (0.278393 + (0.230389 + 0.078108 * (a * a)) * a) * a;
	x *= x;
	return s - s / (x * x);
}

float gaussian(float x, float sigma) {
	return exp(-(x * x) / (2.0 * sigma * sigma)) / (2.50662827463 * sigma);
}

// The blurred rounded box of Evan Wallace: exact along x, four samples
// along y.
float shadowX(float x, float y, float sigma, float corner, vec2 h) {
	float delta = min(h.y - corner - abs(y), 0.0);
	float curved = h.x - corner + sqrt(max(0.0, corner * corner - delta * delta));
	vec2 integral = 0.5 + 0.5 * erf2((x + vec2(-curved, curved)) * (sqrt(0.5) / sigma));
	return integral.y - integral.x;
}

float boxShadow(vec2 p, vec4 rect, float sigma, float corner) {
	vec2 h = rect.zw * 0.5;
	p -= rect.xy + h;
	float low = p.y - h.y;
	float high = p.y + h.y;
	float y0 = clamp(-3.0 * sigma, low, high);
	float y1 = clamp(3.0 * sigma, low, high);
	float dy = (y1 - y0) / 4.0;
	float y = y0 + dy * 0.5;
	float v = 0.0;
	for (int k = 0; k < 4; k++) {
		v += shadowX(p.x, p.y - y, sigma, corner, h) * gaussian(y, sigma) * dy;
		y += dy;
	}
	return v;
}

void main() {
	vec2 p = vPoint.xy;
	vec2 tex = vPoint.zw;
	float kind = vParams.x;
	vec4 res;
	if (kind < 0.5) {
		float outer = coverage(sdRoundRect(p, vRect, vRadii));
		vec4 c = vColor;
		if (vParams.z > 0.5) {
			vec2 d = vGrad.zw - vGrad.xy;
			float t = clamp(dot(p - vGrad.xy, d) / max(dot(d, d), 0.0001), 0.0, 1.0);
			c = mix(vColor, vColor2, t);
		}
		res = premul(c) * outer;
		float bw = vParams.y;
		if (bw > 0.0) {
			vec4 ir = vec4(vRect.xy + bw, vRect.zw - 2.0 * bw);
			float innerCov = (ir.z > 0.0 && ir.w > 0.0) ? coverage(sdRoundRect(p, ir, vInner)) : 0.0;
			float bc = clamp(outer - innerCov, 0.0, 1.0);
			vec4 b = premul(vBorder) * bc;
			res = b + res * (1.0 - b.a);
		}
	} else if (kind < 1.5) {
		float corner = max(max(vRadii.x, vRadii.y), max(vRadii.z, vRadii.w));
		res = premul(vColor) * boxShadow(p, vRect, vParams.z, corner);
	} else if (kind < 2.5) {
		res = premul(vColor) * texture(uMask, tex).r;
	} else if (kind < 3.5) {
		res = texture(uColor, tex) * vColor.a;
	} else {
		res = texture(uImage, tex) * coverage(sdRoundRect(p, vRect, vRadii));
	}
	float clip = coverage(sdRoundRect(p, vClip, vClipRadii));
	fragColor = res * clip * vParams.w;
}

#endif
