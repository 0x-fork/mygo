//go:build linux && (amd64 || arm64)

// Package gl draws scenes with OpenGL 3.3 or OpenGL ES 3.0 into the
// framebuffer bound when Render is called: the one GTK's GtkGLArea binds
// while it emits its render signal. Every op of a scene is an instanced
// quad drawn by one shader (shader.glsl, compiled when the renderer
// starts), from the instances package gpu builds; clips are scissor
// rectangles, with the innermost rounded clip computed in the shader, as
// in packages d3d11 and metal. GL functions come from libepoxy, which GTK
// resolves its own through, called with purego (no cgo).
package gl

import (
	_ "embed"
	"fmt"
	"image"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/gpu"
	"github.com/egoist/mygo/internal/scene"
)

//go:embed shader.glsl
var shaderSource string

// OpenGL enumerations.
const (
	glTriangleStrip      = 0x0005
	glBlend              = 0x0BE2
	glScissorTest        = 0x0C11
	glOne                = 1
	glOneMinusSrcAlpha   = 0x0303
	glOneMinusSrc1Color  = 0x88FA
	glOneMinusSrc1Alpha  = 0x88FB
	glColorBufferBit     = 0x4000
	glArrayBuffer        = 0x8892
	glStreamDraw         = 0x88E0
	glFloat              = 0x1406
	glUnsignedByte       = 0x1401
	glTexture2D          = 0x0DE1
	glTexture0           = 0x84C0
	glTextureMinFilter   = 0x2801
	glTextureMagFilter   = 0x2800
	glTextureWrapS       = 0x2802
	glTextureWrapT       = 0x2803
	glLinear             = 0x2601
	glClampToEdge        = 0x812F
	glR8                 = 0x8229
	glRed                = 0x1903
	glRGBA8              = 0x8058
	glRGBA               = 0x1908
	glUnpackAlignment    = 0x0CF5
	glUnpackRowLength    = 0x0CF2
	glVertexShader       = 0x8B31
	glFragmentShader     = 0x8B30
	glCompileStatus      = 0x8B81
	glLinkStatus         = 0x8B82
	glInfoLogLength      = 0x8B84
	glMaxTextureSize     = 0x0D33
	glVersion            = 0x1F02
	glRenderer           = 0x1F01
	glFramebuffer        = 0x8D40
	glColorAttachment0   = 0x8CE0
	glFramebufferDone    = 0x8CD5
	glFramebufferBinding = 0x8CA6
	glPackAlignment      = 0x0D05
	glReadFramebuffer    = 0x8CA8
	glDrawFramebuffer    = 0x8CA9
	glNearest            = 0x2600
	glNoError            = 0
)

// The functions of libepoxy and GL this package calls.
var (
	loadOnce sync.Once
	errLoad  error

	epoxyGLVersion    func() int32
	epoxyIsDesktopGL  func() bool
	epoxyHasExtension func(name string) bool
	currentGDKContext func() uintptr // gdk_gl_context_get_current, when GDK is loaded

	glGetString             func(name uint32) uintptr
	glGetError              func() uint32
	glGetIntegerv           func(name uint32, data *int32)
	glCreateShader          func(typ uint32) uint32
	glShaderSource          func(shader uint32, count int32, sources **byte, lengths *int32)
	glCompileShader         func(shader uint32)
	glGetShaderiv           func(shader, name uint32, out *int32)
	glGetShaderInfoLog      func(shader uint32, size int32, length *int32, log *byte)
	glDeleteShader          func(shader uint32)
	glCreateProgram         func() uint32
	glAttachShader          func(program, shader uint32)
	glLinkProgram           func(program uint32)
	glGetProgramiv          func(program, name uint32, out *int32)
	glGetProgramInfoLog     func(program uint32, size int32, length *int32, log *byte)
	glUseProgram            func(program uint32)
	glDeleteProgram         func(program uint32)
	glGetUniformLocation    func(program uint32, name string) int32
	glUniform1i             func(location, v int32)
	glUniform2f             func(location int32, x, y float32)
	glGenVertexArrays       func(n int32, out *uint32)
	glBindVertexArray       func(array uint32)
	glDeleteVertexArrays    func(n int32, arrays *uint32)
	glGenBuffers            func(n int32, out *uint32)
	glBindBuffer            func(target, buffer uint32)
	glBufferData            func(target uint32, size int, data unsafe.Pointer, usage uint32)
	glDeleteBuffers         func(n int32, buffers *uint32)
	glEnableVertexAttrib    func(index uint32)
	glVertexAttribPointer   func(index uint32, size int32, typ uint32, normalized bool, stride int32, offset uintptr)
	glVertexAttribDivisor   func(index, divisor uint32)
	glGenTextures           func(n int32, out *uint32)
	glBindTexture           func(target, texture uint32)
	glActiveTexture         func(unit uint32)
	glTexImage2D            func(target uint32, level, internalFormat, w, h, border int32, format, typ uint32, pix unsafe.Pointer)
	glTexSubImage2D         func(target uint32, level, x, y, w, h int32, format, typ uint32, pix unsafe.Pointer)
	glTexParameteri         func(target, name uint32, v int32)
	glDeleteTextures        func(n int32, textures *uint32)
	glPixelStorei           func(name uint32, v int32)
	glViewport              func(x, y, w, h int32)
	glScissor               func(x, y, w, h int32)
	glEnable                func(cap uint32)
	glDisable               func(cap uint32)
	glBlendFunc             func(src, dst uint32)
	glBlendFuncSeparate     func(srcRGB, dstRGB, srcAlpha, dstAlpha uint32)
	glClearColor            func(r, g, b, a float32)
	glClear                 func(mask uint32)
	glDrawArraysInstanced   func(mode uint32, first, count, instances int32)
	glReadPixels            func(x, y, w, h int32, format, typ uint32, pix unsafe.Pointer)
	glGenFramebuffers       func(n int32, out *uint32)
	glBindFramebuffer       func(target, fb uint32)
	glFramebufferTexture2D  func(target, attachment, texTarget, texture uint32, level int32)
	glCheckFramebufferState func(target uint32) uint32
	glDeleteFramebuffers    func(n int32, fbs *uint32)
	glBlitFramebuffer       func(sx0, sy0, sx1, sy1, dx0, dy0, dx1, dy1 int32, mask, filter uint32)
)

func load() error {
	loadOnce.Do(func() {
		lib, err := purego.Dlopen("libepoxy.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			errLoad = fmt.Errorf("gl: cannot load libepoxy: %w", err)
			return
		}
		var missing []string
		fn := func(f any, name string) {
			sym, err := purego.Dlsym(lib, name)
			if err != nil || sym == 0 {
				missing = append(missing, name)
				return
			}
			purego.RegisterFunc(f, sym)
		}
		// libepoxy exports a pointer for each GL function, to a stub that
		// finds the function of the current context when first called.
		gl := func(f any, name string) {
			sym, err := purego.Dlsym(lib, "epoxy_"+name)
			if err != nil || sym == 0 {
				missing = append(missing, name)
				return
			}
			purego.RegisterFunc(f, **(**uintptr)(unsafe.Pointer(&sym)))
		}
		fn(&epoxyGLVersion, "epoxy_gl_version")
		fn(&epoxyIsDesktopGL, "epoxy_is_desktop_gl")
		fn(&epoxyHasExtension, "epoxy_has_gl_extension")
		gl(&glGetString, "glGetString")
		gl(&glGetError, "glGetError")
		gl(&glGetIntegerv, "glGetIntegerv")
		gl(&glCreateShader, "glCreateShader")
		gl(&glShaderSource, "glShaderSource")
		gl(&glCompileShader, "glCompileShader")
		gl(&glGetShaderiv, "glGetShaderiv")
		gl(&glGetShaderInfoLog, "glGetShaderInfoLog")
		gl(&glDeleteShader, "glDeleteShader")
		gl(&glCreateProgram, "glCreateProgram")
		gl(&glAttachShader, "glAttachShader")
		gl(&glLinkProgram, "glLinkProgram")
		gl(&glGetProgramiv, "glGetProgramiv")
		gl(&glGetProgramInfoLog, "glGetProgramInfoLog")
		gl(&glUseProgram, "glUseProgram")
		gl(&glDeleteProgram, "glDeleteProgram")
		gl(&glGetUniformLocation, "glGetUniformLocation")
		gl(&glUniform1i, "glUniform1i")
		gl(&glUniform2f, "glUniform2f")
		gl(&glGenVertexArrays, "glGenVertexArrays")
		gl(&glBindVertexArray, "glBindVertexArray")
		gl(&glDeleteVertexArrays, "glDeleteVertexArrays")
		gl(&glGenBuffers, "glGenBuffers")
		gl(&glBindBuffer, "glBindBuffer")
		gl(&glBufferData, "glBufferData")
		gl(&glDeleteBuffers, "glDeleteBuffers")
		gl(&glEnableVertexAttrib, "glEnableVertexAttribArray")
		gl(&glVertexAttribPointer, "glVertexAttribPointer")
		gl(&glVertexAttribDivisor, "glVertexAttribDivisor")
		gl(&glGenTextures, "glGenTextures")
		gl(&glBindTexture, "glBindTexture")
		gl(&glActiveTexture, "glActiveTexture")
		gl(&glTexImage2D, "glTexImage2D")
		gl(&glTexSubImage2D, "glTexSubImage2D")
		gl(&glTexParameteri, "glTexParameteri")
		gl(&glDeleteTextures, "glDeleteTextures")
		gl(&glPixelStorei, "glPixelStorei")
		gl(&glViewport, "glViewport")
		gl(&glScissor, "glScissor")
		gl(&glEnable, "glEnable")
		gl(&glDisable, "glDisable")
		gl(&glBlendFunc, "glBlendFunc")
		gl(&glBlendFuncSeparate, "glBlendFuncSeparate")
		gl(&glClearColor, "glClearColor")
		gl(&glClear, "glClear")
		gl(&glDrawArraysInstanced, "glDrawArraysInstanced")
		gl(&glReadPixels, "glReadPixels")
		gl(&glGenFramebuffers, "glGenFramebuffers")
		gl(&glBindFramebuffer, "glBindFramebuffer")
		gl(&glFramebufferTexture2D, "glFramebufferTexture2D")
		gl(&glCheckFramebufferState, "glCheckFramebufferStatus")
		gl(&glDeleteFramebuffers, "glDeleteFramebuffers")
		gl(&glBlitFramebuffer, "glBlitFramebuffer")
		if len(missing) > 0 {
			errLoad = fmt.Errorf("gl: libepoxy lacks %s", strings.Join(missing, ", "))
			return
		}
		// GDK is loaded in apps, where its context tells when GTK
		// realized the area again; tests make contexts of their own.
		if sym, err := purego.Dlsym(purego.RTLD_DEFAULT, "gdk_gl_context_get_current"); err == nil && sym != 0 {
			purego.RegisterFunc(&currentGDKContext, sym)
		}
	})
	return errLoad
}

func currentContext() uintptr {
	if currentGDKContext == nil {
		return 0
	}
	return currentGDKContext()
}

func goString(p uintptr) string {
	if p == 0 {
		return ""
	}
	var b []byte
	for i := uintptr(0); ; i++ {
		c := *(*byte)(unsafe.Add(*(*unsafe.Pointer)(unsafe.Pointer(&p)), i))
		if c == 0 {
			return string(b)
		}
		b = append(b, c)
	}
}

type texture struct {
	tex       uint32
	w, h      int
	gen, ver  uint64
	lastFrame uint64
}

// Renderer draws scenes into the framebuffer bound when Render is called,
// with the GL context that was current when New was called. All its
// methods must be called with that context current.
type Renderer struct {
	context uintptr // GDK's, to tell when the area's context changes
	es      bool
	// dual tells that the context blends with a second color from the
	// shader (OpenGL 3.3, or GLES with EXT_blend_func_extended).
	dual     bool
	maxSize  int
	program  uint32
	vao, buf uint32
	uSize    int32
	mask     texture
	color    texture
	empty    uint32 // a texture to bind where there is none
	images   map[uint64]*texture
	frame    uint64

	b gpu.Builder
}

// New returns a renderer for the current GL context.
func New() (*Renderer, error) {
	if err := load(); err != nil {
		return nil, err
	}
	r := &Renderer{images: map[uint64]*texture{}, context: currentContext()}
	if err := r.init(); err != nil {
		r.Release()
		return nil, err
	}
	return r, nil
}

// init creates the program, buffers and textures in the current context.
func (r *Renderer) init() error {
	version := epoxyGLVersion()
	r.es = !epoxyIsDesktopGL()
	if r.es && version < 30 || !r.es && version < 33 {
		return fmt.Errorf("gl: OpenGL %d.%d is too old (%s)", version/10, version%10, goString(glGetString(glVersion)))
	}
	var size int32
	glGetIntegerv(glMaxTextureSize, &size)
	r.maxSize = int(size)
	r.dual = !r.es || epoxyHasExtension("GL_EXT_blend_func_extended")
	header := "#version 330 core\n#define DUAL\n"
	if r.es {
		header = "#version 300 es\n"
		if r.dual {
			header += "#extension GL_EXT_blend_func_extended : require\n#define DUAL\n"
		}
		header += "precision highp float;\nprecision highp int;\n"
	}
	vs, err := compile(glVertexShader, header+"#define VERTEX\n"+shaderSource)
	if err != nil {
		return err
	}
	defer glDeleteShader(vs)
	fs, err := compile(glFragmentShader, header+"#define FRAGMENT\n"+shaderSource)
	if err != nil {
		return err
	}
	defer glDeleteShader(fs)
	r.program = glCreateProgram()
	glAttachShader(r.program, vs)
	glAttachShader(r.program, fs)
	glLinkProgram(r.program)
	var ok int32
	if glGetProgramiv(r.program, glLinkStatus, &ok); ok == 0 {
		return fmt.Errorf("gl: cannot link the shader: %s", infoLog(r.program, glGetProgramiv, glGetProgramInfoLog))
	}
	glUseProgram(r.program)
	r.uSize = glGetUniformLocation(r.program, "uSize")
	glUniform1i(glGetUniformLocation(r.program, "uMask"), 0)
	glUniform1i(glGetUniformLocation(r.program, "uColor"), 1)
	glUniform1i(glGetUniformLocation(r.program, "uImage"), 2)
	glUseProgram(0)

	// The instance buffer feeds eleven float4 attributes per instance;
	// draws point them at their batch's instances.
	glGenVertexArrays(1, &r.vao)
	glGenBuffers(1, &r.buf)
	glBindVertexArray(r.vao)
	glBindBuffer(glArrayBuffer, r.buf)
	for i := range uint32(attributes) {
		glEnableVertexAttrib(i)
		glVertexAttribDivisor(i, 1)
	}
	glBindVertexArray(0)
	glBindBuffer(glArrayBuffer, 0)

	r.empty = newTexture(glRGBA8, glRGBA, 1, 1, []byte{0, 0, 0, 0}, 4)
	if e := glGetError(); e != glNoError {
		return fmt.Errorf("gl: setting up failed (error %#x)", e)
	}
	return nil
}

// attributes is the number of float4s in a gpu.Instance.
const attributes = gpu.InstanceSize / 16

func compile(kind uint32, src string) (uint32, error) {
	s := glCreateShader(kind)
	b := append([]byte(src), 0)
	p := &b[0]
	glShaderSource(s, 1, &p, nil)
	glCompileShader(s)
	var ok int32
	if glGetShaderiv(s, glCompileStatus, &ok); ok == 0 {
		log := infoLog(s, glGetShaderiv, glGetShaderInfoLog)
		glDeleteShader(s)
		return 0, fmt.Errorf("gl: cannot compile the shader: %s", log)
	}
	return s, nil
}

func infoLog(obj uint32, get func(uint32, uint32, *int32), read func(uint32, int32, *int32, *byte)) string {
	var n int32
	get(obj, glInfoLogLength, &n)
	if n <= 1 {
		return "no log"
	}
	buf := make([]byte, n)
	read(obj, n, nil, &buf[0])
	return strings.TrimSpace(string(buf[:n-1]))
}

func newTexture(internal int32, format uint32, w, h int, pix []byte, stride int) uint32 {
	var tex uint32
	glGenTextures(1, &tex)
	glBindTexture(glTexture2D, tex)
	glTexParameteri(glTexture2D, glTextureMinFilter, glLinear)
	glTexParameteri(glTexture2D, glTextureMagFilter, glLinear)
	glTexParameteri(glTexture2D, glTextureWrapS, glClampToEdge)
	glTexParameteri(glTexture2D, glTextureWrapT, glClampToEdge)
	var data unsafe.Pointer
	if len(pix) > 0 {
		data = unsafe.Pointer(&pix[0])
		unpack(stride, internal)
	}
	glTexImage2D(glTexture2D, 0, internal, int32(w), int32(h), 0, format, glUnsignedByte, data)
	unpackReset()
	return tex
}

// unpack sets the layout of the pixels uploads read: rows of stride bytes.
func unpack(stride int, internal int32) {
	bpp := 4
	if internal == glR8 {
		bpp = 1
	}
	glPixelStorei(glUnpackAlignment, 1)
	glPixelStorei(glUnpackRowLength, int32(stride/bpp))
}

func unpackReset() {
	glPixelStorei(glUnpackAlignment, 4)
	glPixelStorei(glUnpackRowLength, 0)
}

func (r *Renderer) syncAtlas(t *texture, a *scene.Atlas, internal int32, format uint32) error {
	if a == nil {
		return nil
	}
	if a.W > r.maxSize || a.H > r.maxSize {
		return fmt.Errorf("gl: a %d×%d atlas is larger than textures can be", a.W, a.H)
	}
	if t.tex == 0 || t.gen != a.Generation() || t.w != a.W || t.h != a.H {
		if t.tex != 0 {
			glDeleteTextures(1, &t.tex)
		}
		t.tex = newTexture(internal, format, a.W, a.H, a.Pix, a.W*a.BPP)
		t.w, t.h, t.gen, t.ver = a.W, a.H, a.Generation(), a.Version()
		return nil
	}
	rects, full := a.Changes(t.gen, t.ver)
	if full {
		rects = []image.Rectangle{image.Rect(0, 0, a.W, a.H)}
	}
	if len(rects) > 0 {
		glBindTexture(glTexture2D, t.tex)
		unpack(a.W*a.BPP, internal)
		for _, rc := range rects {
			src := a.Pix[(rc.Min.Y*a.W+rc.Min.X)*a.BPP:]
			glTexSubImage2D(glTexture2D, 0, int32(rc.Min.X), int32(rc.Min.Y), int32(rc.Dx()), int32(rc.Dy()), format, glUnsignedByte, unsafe.Pointer(&src[0]))
		}
		unpackReset()
	}
	t.ver = a.Version()
	return nil
}

func (r *Renderer) imageTexture(img *scene.Image) uintptr {
	if img.W > r.maxSize || img.H > r.maxSize {
		return 0
	}
	t := r.images[img.ID()]
	if t == nil {
		t = &texture{tex: newTexture(glRGBA8, glRGBA, img.W, img.H, img.Pix, img.W*4), w: img.W, h: img.H, ver: img.Version()}
		r.images[img.ID()] = t
	} else if t.ver != img.Version() {
		glBindTexture(glTexture2D, t.tex)
		unpack(img.W*4, glRGBA8)
		glTexSubImage2D(glTexture2D, 0, 0, 0, int32(img.W), int32(img.H), glRGBA, glUnsignedByte, unsafe.Pointer(&img.Pix[0]))
		unpackReset()
		t.ver = img.Version()
	}
	t.lastFrame = r.frame
	return uintptr(t.tex)
}

// Render draws s into the bound framebuffer, which is s.Width×s.Height
// device pixels.
func (r *Renderer) Render(s *scene.Scene) error {
	if s.Width <= 0 || s.Height <= 0 {
		return nil
	}
	if c := currentContext(); c != r.context {
		// GTK realized the area again, with a new context: the old one
		// took the objects with it.
		r.forget()
		r.context = c
		if err := r.init(); err != nil {
			return err
		}
	}
	if err := r.draw(s); err != nil {
		return err
	}
	if e := glGetError(); e != glNoError {
		return fmt.Errorf("gl: drawing failed (error %#x)", e)
	}
	return nil
}

func (r *Renderer) draw(s *scene.Scene) error {
	r.frame++
	glActiveTexture(glTexture0)
	if err := r.syncAtlas(&r.mask, s.MaskAtlas, glR8, glRed); err != nil {
		return err
	}
	if err := r.syncAtlas(&r.color, s.ColorAtlas, glRGBA8, glRGBA); err != nil {
		return err
	}
	r.b.Build(s, r.imageTexture)

	glViewport(0, 0, int32(s.Width), int32(s.Height))
	glDisable(glScissorTest)
	c := s.Clear.Premul(1)
	glClearColor(c[0], c[1], c[2], c[3])
	glClear(glColorBufferBit)
	if len(r.b.Instances) == 0 {
		return nil
	}
	glUseProgram(r.program)
	glUniform2f(r.uSize, float32(s.Width), float32(s.Height))
	glBindVertexArray(r.vao)
	glBindBuffer(glArrayBuffer, r.buf)
	glBufferData(glArrayBuffer, len(r.b.Instances)*gpu.InstanceSize, unsafe.Pointer(&r.b.Instances[0]), glStreamDraw)
	for unit, tex := range [3]uint32{or(r.mask.tex, r.empty), or(r.color.tex, r.empty), r.empty} {
		glActiveTexture(glTexture0 + uint32(unit))
		glBindTexture(glTexture2D, tex)
	}
	glEnable(glBlend)
	if r.dual {
		glBlendFuncSeparate(glOne, glOneMinusSrc1Color, glOne, glOneMinusSrc1Alpha)
	} else {
		glBlendFunc(glOne, glOneMinusSrcAlpha)
	}
	glEnable(glScissorTest)
	bound := uintptr(r.empty)
	for _, b := range r.b.Batches {
		sc := b.Scissor
		sc.Left, sc.Top = max(sc.Left, 0), max(sc.Top, 0)
		sc.Right, sc.Bottom = min(sc.Right, int32(s.Width)), min(sc.Bottom, int32(s.Height))
		if b.Count == 0 || sc.Empty() {
			continue
		}
		if b.Image != 0 && b.Image != bound {
			bound = b.Image
			glBindTexture(glTexture2D, uint32(bound)) // unit 2 is active
		}
		// Instances draw from the batch's first: without base instances in
		// OpenGL ES 3.0, the attributes start there.
		offset := uintptr(b.Start * gpu.InstanceSize)
		for i := range uint32(attributes) {
			glVertexAttribPointer(i, 4, glFloat, false, int32(gpu.InstanceSize), offset+uintptr(i)*16)
		}
		// GL's scissor rectangles start at the bottom.
		glScissor(sc.Left, int32(s.Height)-sc.Bottom, sc.Right-sc.Left, sc.Bottom-sc.Top)
		glDrawArraysInstanced(glTriangleStrip, 0, 4, int32(b.Count))
	}
	// Leave the state as GTK, which draws the area with the same context,
	// expects it.
	glDisable(glScissorTest)
	glDisable(glBlend)
	for unit := range 3 {
		glActiveTexture(glTexture0 + uint32(unit))
		glBindTexture(glTexture2D, 0)
	}
	glActiveTexture(glTexture0)
	glBindBuffer(glArrayBuffer, 0)
	glBindVertexArray(0)
	glUseProgram(0)
	if r.frame%120 == 0 {
		for key, t := range r.images {
			if r.frame-t.lastFrame > 240 {
				glDeleteTextures(1, &t.tex)
				delete(r.images, key)
			}
		}
	}
	return nil
}

func or(a, b uint32) uint32 {
	if a != 0 {
		return a
	}
	return b
}

// forget drops the objects of a context that is gone.
func (r *Renderer) forget() {
	r.program, r.vao, r.buf, r.empty = 0, 0, 0, 0
	r.mask, r.color = texture{}, texture{}
	clear(r.images)
}

// Release deletes the renderer's objects when its context is current, and
// otherwise leaves them to the context, which deletes them with itself.
func (r *Renderer) Release() {
	if errLoad != nil || currentContext() != r.context {
		r.forget()
		return
	}
	for _, t := range r.images {
		glDeleteTextures(1, &t.tex)
	}
	for _, tex := range []*uint32{&r.mask.tex, &r.color.tex, &r.empty} {
		if *tex != 0 {
			glDeleteTextures(1, tex)
		}
	}
	if r.buf != 0 {
		glDeleteBuffers(1, &r.buf)
	}
	if r.vao != 0 {
		glDeleteVertexArrays(1, &r.vao)
	}
	if r.program != 0 {
		glDeleteProgram(r.program)
	}
	r.forget()
}
