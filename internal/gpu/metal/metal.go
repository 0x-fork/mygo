//go:build darwin

// Package metal draws scenes with Metal into a CAMetalLayer, called through
// the Objective-C runtime with purego like the rest of the macOS backend
// (no cgo). Every op of a scene is an instanced quad drawn by one shader
// (shader.metal, compiled ahead of time into shaderlib.go), from the
// instances package gpu builds; clips are scissor rectangles, with the
// innermost rounded clip computed in the shader, as in package d3d11.
package metal

//go:generate go run gen.go

import (
	_ "embed"
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/internal/gpu"
	"github.com/egoist/mygo/internal/scene"
)

//go:embed shader.metal
var shaderSource string

type id = uintptr

// Metal and Core Graphics structs passed by value.
type (
	mtlRegion      struct{ X, Y, Z, W, H, D uint }
	mtlScissorRect struct{ X, Y, W, H uint }
	mtlClearColor  struct{ R, G, B, A float64 }
	cgSize         struct{ W, H float64 }
	cgRect         struct{ X, Y, W, H float64 }
)

const (
	pixelFormatR8Unorm    = 10
	pixelFormatRGBA8Unorm = 70
	pixelFormatBGRA8Unorm = 80

	usageShaderRead   = 1
	usageRenderTarget = 4
	storageManaged    = 1

	loadActionClear        = 2
	storeActionStore       = 1
	primitiveTriStrip      = 4
	blendOne               = 1
	blendOneMinusSrc1Color = 16
	blendOneMinusSrc1A     = 18
	filterLinear           = 1
	statusError            = 5
	layerWidthSizable      = 1 << 1
	layerHeightSizable     = 1 << 4
)

var (
	loadOnce sync.Once
	errLoad  error

	msgSend           uintptr
	poolPush, poolPop uintptr
	createDevice      func() id
	srgb              uintptr
	msgReplaceRegion  func(obj id, sel objc.SEL, r mtlRegion, level uint, bytes unsafe.Pointer, bytesPerRow uint)
	msgGetBytes       func(obj id, sel objc.SEL, bytes unsafe.Pointer, bytesPerRow uint, r mtlRegion, level uint)
	msgSetScissor     func(obj id, sel objc.SEL, r mtlScissorRect)
	msgSetClearColor  func(obj id, sel objc.SEL, c mtlClearColor)
	msgSetSize        func(obj id, sel objc.SEL, s cgSize)
	msgSetRect        func(obj id, sel objc.SEL, r cgRect)
	msgRect           func(obj id, sel objc.SEL) cgRect
	msgSetFloat       func(obj id, sel objc.SEL, v float64)
	selMu             sync.Mutex
	selectors         = map[string]objc.SEL{}

	dispatchDataCreate, dispatchRelease uintptr

	// IOSurface's functions, for frames drawn in memory.
	ioSurfaceLock, ioSurfaceUnlock, ioSurfaceBaseAddress, ioSurfaceBytesPerRow, ioSurfaceID uintptr

	// Timers that trim the drawables of renderers drawing nothing for a
	// while (see trim), by the handle in their info.
	cfAbsoluteTimeGetCurrent      func() float64
	cfRunLoopTimerCreate          func(alloc uintptr, fireDate, interval float64, flags uint64, order int, callout uintptr, ctx *cfTimerContext) uintptr
	cfRunLoopTimerSetNextFireDate func(timer uintptr, date float64)
	cfRunLoopAddTimer             uintptr
	cfRunLoopGetMain              uintptr
	cfRunLoopTimerInvalidate      uintptr
	cfRelease                     uintptr
	commonModes                   uintptr
	trimCallback                  uintptr
	trimTimers                    = map[uintptr]*Renderer{}
	lastTrimHandle                uintptr
)

// cfTimerContext is CFRunLoopTimerContext.
type cfTimerContext struct {
	version                          int
	info                             uintptr
	retain, release, copyDescription uintptr
}

func load() error {
	loadOnce.Do(func() {
		lib := func(path string) uintptr {
			h, err := purego.Dlopen(path, purego.RTLD_GLOBAL|purego.RTLD_NOW)
			if err != nil && errLoad == nil {
				errLoad = fmt.Errorf("metal: cannot load %s: %w", path, err)
			}
			return h
		}
		objcLib := lib("/usr/lib/libobjc.A.dylib")
		metalLib := lib("/System/Library/Frameworks/Metal.framework/Metal")
		lib("/System/Library/Frameworks/QuartzCore.framework/QuartzCore") // CAMetalLayer
		cg := lib("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
		if errLoad != nil {
			return
		}
		sym := func(h uintptr, name string) uintptr {
			p, err := purego.Dlsym(h, name)
			if err != nil && errLoad == nil {
				errLoad = fmt.Errorf("metal: %w", err)
			}
			return p
		}
		msgSend = sym(objcLib, "objc_msgSend")
		poolPush = sym(objcLib, "objc_autoreleasePoolPush")
		poolPop = sym(objcLib, "objc_autoreleasePoolPop")
		create := sym(metalLib, "MTLCreateSystemDefaultDevice")
		name := sym(cg, "kCGColorSpaceSRGB")
		colorSpace := sym(cg, "CGColorSpaceCreateWithName")
		system := lib("/usr/lib/libSystem.B.dylib")
		cf := lib("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
		ioSurface := lib("/System/Library/Frameworks/IOSurface.framework/IOSurface")
		if errLoad != nil {
			return
		}
		ioSurfaceLock = sym(ioSurface, "IOSurfaceLock")
		ioSurfaceUnlock = sym(ioSurface, "IOSurfaceUnlock")
		ioSurfaceBaseAddress = sym(ioSurface, "IOSurfaceGetBaseAddress")
		ioSurfaceBytesPerRow = sym(ioSurface, "IOSurfaceGetBytesPerRow")
		ioSurfaceID = sym(ioSurface, "IOSurfaceGetID")
		dispatchDataCreate = sym(system, "dispatch_data_create")
		dispatchRelease = sym(system, "dispatch_release")
		timeNow := sym(cf, "CFAbsoluteTimeGetCurrent")
		timerCreate := sym(cf, "CFRunLoopTimerCreate")
		timerSetNext := sym(cf, "CFRunLoopTimerSetNextFireDate")
		cfRunLoopAddTimer = sym(cf, "CFRunLoopAddTimer")
		cfRunLoopGetMain = sym(cf, "CFRunLoopGetMain")
		cfRunLoopTimerInvalidate = sym(cf, "CFRunLoopTimerInvalidate")
		cfRelease = sym(cf, "CFRelease")
		modes := sym(cf, "kCFRunLoopCommonModes")
		// Methods returning structs larger than 16 bytes use another entry
		// point on amd64.
		stret := msgSend
		if runtime.GOARCH == "amd64" {
			stret = sym(objcLib, "objc_msgSend_stret")
		}
		if errLoad != nil {
			return
		}
		purego.RegisterFunc(&createDevice, create)
		purego.RegisterFunc(&cfAbsoluteTimeGetCurrent, timeNow)
		purego.RegisterFunc(&cfRunLoopTimerCreate, timerCreate)
		purego.RegisterFunc(&cfRunLoopTimerSetNextFireDate, timerSetNext)
		commonModes = *(*uintptr)(ptr(modes))
		trimCallback = purego.NewCallback(func(timer, info uintptr) {
			if r := trimTimers[info]; r != nil {
				r.trimIfIdle()
			}
		})
		purego.RegisterFunc(&msgReplaceRegion, msgSend)
		purego.RegisterFunc(&msgGetBytes, msgSend)
		purego.RegisterFunc(&msgSetScissor, msgSend)
		purego.RegisterFunc(&msgSetClearColor, msgSend)
		purego.RegisterFunc(&msgSetSize, msgSend)
		purego.RegisterFunc(&msgSetRect, msgSend)
		purego.RegisterFunc(&msgSetFloat, msgSend)
		purego.RegisterFunc(&msgRect, stret)
		srgb, _, _ = purego.SyscallN(colorSpace, *(*uintptr)(ptr(name)))
	})
	return errLoad
}

func sel(name string) objc.SEL {
	selMu.Lock()
	defer selMu.Unlock()
	s, ok := selectors[name]
	if !ok {
		s = objc.RegisterName(name)
		selectors[name] = s
	}
	return s
}

func class(name string) id { return id(objc.GetClass(name)) }

// ptr turns an address from native code into a pointer.
func ptr(a uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&a)) }

// send sends a message whose arguments are integers or pointers. Go
// pointers converted in the call stay where they are until it returns
// (go:uintptrescapes moves them to the heap).
//
//go:uintptrescapes
func send(obj id, selector string, args ...uintptr) id {
	var a [10]uintptr
	a[0], a[1] = obj, uintptr(sel(selector))
	n := copy(a[2:], args)
	r, _, _ := purego.SyscallN(msgSend, a[:n+2]...)
	return r
}

func release(obj *id) {
	if *obj != 0 {
		send(*obj, "release")
		*obj = 0
	}
}

// pool runs fn in an autorelease pool, which takes the objects Metal
// returns without giving them to the caller.
func pool(fn func()) {
	// The pool belongs to the thread, which the goroutine must not leave
	// before popping it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	p, _, _ := purego.SyscallN(poolPush)
	defer purego.SyscallN(poolPop, p)
	fn()
}

// nsString returns an autoreleased NSString.
func nsString(s string) id {
	b := append([]byte(s), 0)
	return send(class("NSString"), "stringWithUTF8String:", uintptr(unsafe.Pointer(&b[0])))
}

// describe returns the description of an NSError.
func describe(err id) string {
	if err == 0 {
		return "unknown error"
	}
	p := send(send(err, "localizedDescription"), "UTF8String")
	if p == 0 {
		return "unknown error"
	}
	var b []byte
	for i := uintptr(0); ; i++ {
		c := *(*byte)(ptr(p + i))
		if c == 0 {
			return string(b)
		}
		b = append(b, c)
	}
}

// need reports which of the methods obj lacks, so that a renderer fails
// rather than crash on a system without them.
func need(obj id, what string, methods ...string) error {
	if obj == 0 {
		return fmt.Errorf("metal: no %s", what)
	}
	for _, m := range methods {
		if byte(send(obj, "respondsToSelector:", uintptr(sel(m)))) == 0 {
			return fmt.Errorf("metal: %s has no %s", what, m)
		}
	}
	return nil
}

type texture struct {
	tex       id
	w, h      int
	gen, ver  uint64
	lastFrame uint64
}

// Renderer draws scenes into a CAMetalLayer it adds to a view's layer.
type Renderer struct {
	device, queue, pipeline, sampler id
	// layer is the CAMetalLayer in superlayer, the view's.
	layer, superlayer id
	w, h              int
	bounds            cgRect
	scale             float64

	instBuf id
	instCap int
	mask    texture
	color   texture
	empty   id // a texture to bind where there is none
	images  map[uint64]*texture
	frame   uint64
	// last is the command buffer of the last frame, which the next waits
	// for before it changes what the GPU reads.
	last    id
	err     error
	checked bool

	// Frames drawn in memory (PresentPixels): pixelFrames counts them,
	// pixelDamage has what each of the last changed, and shown the frame
	// each drawable holds, by the ID of its IOSurface, unless the GPU drew
	// into it since.
	pixelFrames uint64
	pixelDamage [pixelHistory][]image.Rectangle
	shown       map[uint32]uint64
	// lastGPU is when the GPU last drew a frame.
	lastGPU time.Time

	// lastRender is when the last frame drew; trimTimer trims the
	// drawables once none has for a while, armed until it fires, and
	// trimmed is whether it did.
	lastRender         time.Time
	trimTimer          uintptr
	trimHandle         uintptr
	trimArmed, trimmed bool

	b gpu.Builder
}

// New creates a renderer drawing into a CAMetalLayer that it adds to
// layer, the layer of a view, and sizes as it.
func New(layer uintptr) (r *Renderer, err error) {
	if layer == 0 {
		return nil, errors.New("metal: no layer")
	}
	if r, err = newRenderer(); err != nil {
		return nil, err
	}
	pool(func() {
		if class("CAMetalLayer") == 0 {
			err = errors.New("metal: no CAMetalLayer")
			return
		}
		ml := send(send(class("CAMetalLayer"), "alloc"), "init")
		if err = need(ml, "CAMetalLayer", "setDevice:", "setPixelFormat:", "setFramebufferOnly:", "setPresentsWithTransaction:",
			"setColorspace:", "setDrawableSize:", "setContentsScale:", "setFrame:", "setAutoresizingMask:", "nextDrawable"); err != nil {
			release(&ml)
			return
		}
		send(ml, "setDevice:", r.device)
		send(ml, "setPixelFormat:", pixelFormatBGRA8Unorm)
		// Frames drawn in memory are copied into drawables, which the
		// CPU writes then.
		send(ml, "setFramebufferOnly:", 0)
		// Frames wait for the last to finish, so two drawables do,
		// rather than the three Core Animation makes otherwise while a
		// window animates: one less frame of memory.
		if respondsTo(ml, "setMaximumDrawableCount:") {
			send(ml, "setMaximumDrawableCount:", 2)
		}
		// Frames show with the window's other changes, as during a live
		// resize, instead of a moment after them.
		send(ml, "setPresentsWithTransaction:", 1)
		send(ml, "setColorspace:", srgb)
		send(ml, "setOpaque:", 0)
		send(ml, "setAutoresizingMask:", layerWidthSizable|layerHeightSizable)
		send(layer, "addSublayer:", ml)
		r.layer, r.superlayer = ml, layer
	})
	if err != nil {
		r.Release()
		return nil, err
	}
	return r, nil
}

// newRenderer creates a renderer without a layer, for New and tests.
func newRenderer() (r *Renderer, err error) {
	if err := load(); err != nil {
		return nil, err
	}
	r = &Renderer{images: map[uint64]*texture{}}
	pool(func() { err = r.init() })
	if err != nil {
		r.Release()
		return nil, err
	}
	return r, nil
}

func (r *Renderer) init() error {
	r.device = createDevice() // owned: NS_RETURNS_RETAINED
	if err := need(r.device, "Metal device", "newCommandQueue", "newLibraryWithSource:options:error:",
		"newRenderPipelineStateWithDescriptor:error:", "newSamplerStateWithDescriptor:", "newTextureWithDescriptor:",
		"newBufferWithLength:options:"); err != nil {
		return err
	}
	r.queue = send(r.device, "newCommandQueue")
	if r.queue == 0 {
		return errors.New("metal: no command queue")
	}
	var errObj id
	lib := r.compiledLibrary()
	if lib == 0 {
		lib = send(r.device, "newLibraryWithSource:options:error:", nsString(shaderSource), 0, uintptr(unsafe.Pointer(&errObj)))
	}
	if lib == 0 {
		return fmt.Errorf("metal: cannot compile the shader: %s", describe(errObj))
	}
	defer release(&lib)
	vs := send(lib, "newFunctionWithName:", nsString("vs"))
	defer release(&vs)
	ps := send(lib, "newFunctionWithName:", nsString("ps"))
	defer release(&ps)
	if vs == 0 || ps == 0 {
		return errors.New("metal: the shader has no vs or ps")
	}
	desc := send(send(class("MTLRenderPipelineDescriptor"), "alloc"), "init")
	defer release(&desc)
	send(desc, "setVertexFunction:", vs)
	send(desc, "setFragmentFunction:", ps)
	ca := send(send(desc, "colorAttachments"), "objectAtIndexedSubscript:", 0)
	send(ca, "setPixelFormat:", pixelFormatBGRA8Unorm)
	// Premultiplied colors over what is drawn.
	send(ca, "setBlendingEnabled:", 1)
	send(ca, "setSourceRGBBlendFactor:", blendOne)
	// Dual-source blending: the shader's second color is the source's
	// alpha of each channel.
	send(ca, "setDestinationRGBBlendFactor:", blendOneMinusSrc1Color)
	send(ca, "setSourceAlphaBlendFactor:", blendOne)
	send(ca, "setDestinationAlphaBlendFactor:", blendOneMinusSrc1A)
	errObj = 0
	r.pipeline = send(r.device, "newRenderPipelineStateWithDescriptor:error:", desc, uintptr(unsafe.Pointer(&errObj)))
	if r.pipeline == 0 {
		return fmt.Errorf("metal: cannot create the pipeline: %s", describe(errObj))
	}
	sd := send(send(class("MTLSamplerDescriptor"), "alloc"), "init")
	defer release(&sd)
	send(sd, "setMinFilter:", filterLinear)
	send(sd, "setMagFilter:", filterLinear)
	r.sampler = send(r.device, "newSamplerStateWithDescriptor:", sd)
	if r.empty = r.newTexture(1, 1, pixelFormatRGBA8Unorm, usageShaderRead, []byte{0, 0, 0, 0}, 4); r.empty == 0 || r.sampler == 0 {
		return errors.New("metal: cannot create a texture")
	}
	return nil
}

// compiledLibrary returns the library compiled from the shader ahead of
// time, or 0 when it was compiled from another shader.metal (go generate
// was not run since it changed) or Metal cannot load it.
func (r *Renderer) compiledLibrary() id {
	if gpu.SourceSum(shaderSource) != shaderLibrarySum || dispatchDataCreate == 0 || !respondsTo(r.device, "newLibraryWithData:error:") {
		return 0
	}
	// Without a destructor, dispatch_data_create copies the bytes.
	data, _, _ := purego.SyscallN(dispatchDataCreate, uintptr(unsafe.Pointer(&shaderLibrary[0])), uintptr(len(shaderLibrary)), 0, 0)
	if data == 0 {
		return 0
	}
	defer purego.SyscallN(dispatchRelease, data)
	var errObj id
	return send(r.device, "newLibraryWithData:error:", data, uintptr(unsafe.Pointer(&errObj)))
}

func respondsTo(obj id, method string) bool {
	return byte(send(obj, "respondsToSelector:", uintptr(sel(method)))) != 0
}

// newTexture returns an owned w×h texture holding pix, rows of stride
// bytes, when given.
func (r *Renderer) newTexture(w, h int, format, usage uint, pix []byte, stride int) id {
	desc := send(class("MTLTextureDescriptor"), "texture2DDescriptorWithPixelFormat:width:height:mipmapped:", uintptr(format), uintptr(w), uintptr(h), 0)
	if desc == 0 {
		return 0
	}
	send(desc, "setUsage:", uintptr(usage))
	if usage&usageRenderTarget != 0 {
		send(desc, "setStorageMode:", storageManaged)
	}
	tex := send(r.device, "newTextureWithDescriptor:", desc)
	if tex != 0 && len(pix) > 0 {
		msgReplaceRegion(tex, sel("replaceRegion:mipmapLevel:withBytes:bytesPerRow:"), mtlRegion{W: uint(w), H: uint(h), D: 1}, 0, unsafe.Pointer(&pix[0]), uint(stride))
	}
	return tex
}

// syncAtlas uploads what changed in an atlas since the texture had it.
func (r *Renderer) syncAtlas(t *texture, a *scene.Atlas, format uint) error {
	if a == nil {
		return nil
	}
	if t.tex == 0 || t.gen != a.Generation() || t.w != a.W || t.h != a.H {
		release(&t.tex)
		if t.tex = r.newTexture(a.W, a.H, format, usageShaderRead, a.Pix, a.W*a.BPP); t.tex == 0 {
			return errors.New("metal: cannot create an atlas texture")
		}
		t.w, t.h, t.gen, t.ver = a.W, a.H, a.Generation(), a.Version()
		return nil
	}
	rects, full := a.Changes(t.gen, t.ver)
	if full {
		rects = []image.Rectangle{image.Rect(0, 0, a.W, a.H)}
	}
	for _, rc := range rects {
		src := a.Pix[(rc.Min.Y*a.W+rc.Min.X)*a.BPP:]
		region := mtlRegion{X: uint(rc.Min.X), Y: uint(rc.Min.Y), W: uint(rc.Dx()), H: uint(rc.Dy()), D: 1}
		msgReplaceRegion(t.tex, sel("replaceRegion:mipmapLevel:withBytes:bytesPerRow:"), region, 0, unsafe.Pointer(&src[0]), uint(a.W*a.BPP))
	}
	t.ver = a.Version()
	return nil
}

// imageTexture returns the texture of an image, uploading it when new or
// changed.
func (r *Renderer) imageTexture(img *scene.Image) uintptr {
	t := r.images[img.ID()]
	if t == nil {
		tex := r.newTexture(img.W, img.H, pixelFormatRGBA8Unorm, usageShaderRead, img.Pix, img.W*4)
		if tex == 0 {
			return 0
		}
		t = &texture{tex: tex, w: img.W, h: img.H, ver: img.Version()}
		r.images[img.ID()] = t
	} else if t.ver != img.Version() {
		msgReplaceRegion(t.tex, sel("replaceRegion:mipmapLevel:withBytes:bytesPerRow:"), mtlRegion{W: uint(img.W), H: uint(img.H), D: 1}, 0, unsafe.Pointer(&img.Pix[0]), uint(img.W*4))
		t.ver = img.Version()
	}
	t.lastFrame = r.frame
	return t.tex
}

// waitLast waits for the GPU to finish the last frame, so that this one
// can change the buffers and textures it read.
func (r *Renderer) waitLast() {
	if r.last == 0 {
		return
	}
	send(r.last, "waitUntilCompleted")
	if int(send(r.last, "status")) == statusError && r.err == nil {
		r.err = fmt.Errorf("metal: a frame failed: %s", describe(send(r.last, "error")))
	}
	release(&r.last)
}

// encode encodes drawing s into target, returning the autoreleased
// command buffer, not yet committed.
func (r *Renderer) encode(s *scene.Scene, target id) (id, error) {
	r.frame++
	if err := r.syncAtlas(&r.mask, s.MaskAtlas, pixelFormatR8Unorm); err != nil {
		return 0, err
	}
	if err := r.syncAtlas(&r.color, s.ColorAtlas, pixelFormatRGBA8Unorm); err != nil {
		return 0, err
	}
	r.b.Build(s, r.imageTexture)
	if n := len(r.b.Instances); n > r.instCap {
		release(&r.instBuf)
		capacity := max(n*3/2, 1024)
		r.instBuf = send(r.device, "newBufferWithLength:options:", uintptr(capacity*gpu.InstanceSize), 0) // shared storage
		if r.instBuf == 0 {
			r.instCap = 0
			return 0, errors.New("metal: cannot create the instance buffer")
		}
		r.instCap = capacity
	}
	if n := len(r.b.Instances); n > 0 {
		dst := unsafe.Slice((*gpu.Instance)(ptr(send(r.instBuf, "contents"))), n)
		copy(dst, r.b.Instances)
	}

	rpd := send(class("MTLRenderPassDescriptor"), "renderPassDescriptor")
	ca := send(send(rpd, "colorAttachments"), "objectAtIndexedSubscript:", 0)
	send(ca, "setTexture:", target)
	send(ca, "setLoadAction:", loadActionClear)
	send(ca, "setStoreAction:", storeActionStore)
	c := s.Clear.Premul(1)
	msgSetClearColor(ca, sel("setClearColor:"), mtlClearColor{float64(c[0]), float64(c[1]), float64(c[2]), float64(c[3])})
	cb := send(r.queue, "commandBuffer")
	if !r.checked {
		if err := need(cb, "command buffer", "renderCommandEncoderWithDescriptor:", "commit", "waitUntilScheduled", "waitUntilCompleted", "status", "blitCommandEncoder"); err != nil {
			return 0, err
		}
	}
	enc := send(cb, "renderCommandEncoderWithDescriptor:", rpd)
	if !r.checked {
		if err := need(enc, "render encoder", "setRenderPipelineState:", "setVertexBytes:length:atIndex:", "setVertexBuffer:offset:atIndex:",
			"setFragmentBuffer:offset:atIndex:", "setFragmentTexture:atIndex:", "setFragmentSamplerState:atIndex:", "setScissorRect:",
			"drawPrimitives:vertexStart:vertexCount:instanceCount:", "endEncoding"); err != nil {
			if enc != 0 {
				send(enc, "endEncoding")
			}
			return 0, err
		}
		r.checked = true
	}
	send(enc, "setRenderPipelineState:", r.pipeline)
	globals := [4]float32{float32(s.Width), float32(s.Height), 0, 0}
	send(enc, "setVertexBytes:length:atIndex:", uintptr(unsafe.Pointer(&globals[0])), unsafe.Sizeof(globals), 1)
	send(enc, "setFragmentSamplerState:atIndex:", r.sampler, 0)
	send(enc, "setFragmentTexture:atIndex:", or(r.mask.tex, r.empty), 0)
	send(enc, "setFragmentTexture:atIndex:", or(r.color.tex, r.empty), 1)
	send(enc, "setFragmentTexture:atIndex:", r.empty, 2)
	bound := r.empty
	for _, b := range r.b.Batches {
		// Metal requires scissor rectangles within the target.
		sc := b.Scissor
		sc.Left, sc.Top = max(sc.Left, 0), max(sc.Top, 0)
		sc.Right, sc.Bottom = min(sc.Right, int32(s.Width)), min(sc.Bottom, int32(s.Height))
		if b.Count == 0 || sc.Empty() {
			continue
		}
		if b.Image != 0 && b.Image != bound {
			bound = b.Image
			send(enc, "setFragmentTexture:atIndex:", bound, 2)
		}
		offset := uintptr(b.Start * gpu.InstanceSize)
		send(enc, "setVertexBuffer:offset:atIndex:", r.instBuf, offset, 0)
		send(enc, "setFragmentBuffer:offset:atIndex:", r.instBuf, offset, 0)
		msgSetScissor(enc, sel("setScissorRect:"), mtlScissorRect{uint(sc.Left), uint(sc.Top), uint(sc.Right - sc.Left), uint(sc.Bottom - sc.Top)})
		send(enc, "drawPrimitives:vertexStart:vertexCount:instanceCount:", primitiveTriStrip, 0, 4, uintptr(b.Count))
	}
	send(enc, "endEncoding")
	return cb, nil
}

func or(a, b id) id {
	if a != 0 {
		return a
	}
	return b
}

// Render draws s into the layer and presents it.
func (r *Renderer) Render(s *scene.Scene) (err error) {
	if s.Width <= 0 || s.Height <= 0 || r.layer == 0 {
		return nil
	}
	pool(func() { err = r.render(s) })
	return err
}

func (r *Renderer) render(s *scene.Scene) error {
	r.waitLast()
	if r.err != nil {
		return r.err
	}
	scale := float64(s.Scale)
	if scale <= 0 {
		scale = 1
	}
	r.fit(s.Width, s.Height, scale)
	drawable := send(r.layer, "nextDrawable")
	if drawable == 0 {
		return nil // none came within a second: skip the frame
	}
	texture := send(drawable, "texture")
	// The GPU draws all of the drawable, which holds no frame drawn in
	// memory anymore.
	if surface := send(texture, "iosurface"); surface != 0 {
		delete(r.shown, surfaceID(surface))
	}
	cb, err := r.encode(s, texture)
	if err != nil {
		return err
	}
	send(cb, "commit")
	send(cb, "waitUntilScheduled")
	send(drawable, "present")
	r.last = send(cb, "retain")
	r.lastRender = time.Now()
	r.lastGPU = r.lastRender
	r.armTrim()
	// Forget the textures of images no frame drew for a while.
	if r.frame%120 == 0 {
		for key, t := range r.images {
			if r.frame-t.lastFrame > 240 {
				release(&t.tex)
				delete(r.images, key)
			}
		}
	}
	return nil
}

// fit sizes the layer and its drawables for frames of w×h pixels at
// scale, and as its view.
func (r *Renderer) fit(w, h int, scale float64) {
	bounds := msgRect(r.superlayer, sel("bounds"))
	if w == r.w && h == r.h && scale == r.scale && bounds == r.bounds && !r.trimmed {
		return
	}
	// Without animating the change, as layers do by default.
	tx := class("CATransaction")
	send(tx, "begin")
	send(tx, "setDisableActions:", 1)
	msgSetRect(r.layer, sel("setFrame:"), bounds)
	msgSetFloat(r.layer, sel("setContentsScale:"), scale)
	msgSetSize(r.layer, sel("setDrawableSize:"), cgSize{float64(w), float64(h)})
	send(tx, "commit")
	r.w, r.h, r.scale, r.bounds, r.trimmed = w, h, scale, bounds, false
	clear(r.shown) // new drawables
}

// pixelHistory is how many frames drawn in memory a drawable may lag
// behind for PresentPixels to copy only what changed since.
const pixelHistory = 4

// PresentPixels shows a frame drawn in memory without the GPU, which takes
// longer to start than the CPU takes to draw small changes, and whose
// driver holds memory for a couple of seconds after each frame it draws.
// pix holds rows of stride bytes of premultiplied BGRA, width×height pixels
// at scale, and damage is where it changed since the last frame drawn in
// memory. Only what changed since the frame the next drawable holds goes
// into it, through its IOSurface.
func (r *Renderer) PresentPixels(pix []byte, stride, width, height int, scale float64, damage []image.Rectangle) (err error) {
	if width <= 0 || height <= 0 || r.layer == 0 {
		return nil
	}
	if ioSurfaceLock == 0 || len(pix) < (height-1)*stride+width*4 {
		return errors.New("metal: cannot present pixels")
	}
	pool(func() { err = r.presentPixels(pix, stride, width, height, scale, damage) })
	return err
}

func (r *Renderer) presentPixels(pix []byte, stride, width, height int, scale float64, damage []image.Rectangle) error {
	r.waitLast()
	if r.err != nil {
		return r.err
	}
	if scale <= 0 {
		scale = 1
	}
	r.fit(width, height, scale)
	r.pixelFrames++
	n := r.pixelFrames
	r.pixelDamage[n%pixelHistory] = append(r.pixelDamage[n%pixelHistory][:0], damage...)
	drawable := send(r.layer, "nextDrawable")
	if drawable == 0 {
		return nil // none came within a second: skip the frame
	}
	surface := send(send(drawable, "texture"), "iosurface")
	if surface == 0 {
		return errors.New("metal: a drawable without an IOSurface")
	}
	if kr, _, _ := purego.SyscallN(ioSurfaceLock, surface, 0, 0); kr != 0 {
		return fmt.Errorf("metal: cannot lock a drawable (%#x)", kr)
	}
	base, _, _ := purego.SyscallN(ioSurfaceBaseAddress, surface)
	bpr, _, _ := purego.SyscallN(ioSurfaceBytesPerRow, surface)
	dst := unsafe.Slice((*byte)(ptr(base)), int(bpr)*height)
	key := surfaceID(surface)
	frame := image.Rect(0, 0, width, height)
	for _, rc := range r.changedSince(key, n, width, height) {
		copyRect(dst, int(bpr), pix, stride, rc.Intersect(frame))
	}
	purego.SyscallN(ioSurfaceUnlock, surface, 0, 0)
	if r.shown == nil {
		r.shown = map[uint32]uint64{}
	}
	r.shown[key] = n
	send(drawable, "present")
	r.lastRender = time.Now()
	r.armTrim()
	if !r.lastGPU.IsZero() && r.lastRender.Sub(r.lastGPU) > trimAfter {
		r.releaseTextures()
		r.lastGPU = time.Time{}
	}
	return nil
}

// releaseTextures frees what only frames the GPU draws use, once only
// frames drawn in memory came for a while; the next frame the GPU draws
// makes them again, uploading the atlases whole.
func (r *Renderer) releaseTextures() {
	release(&r.mask.tex)
	release(&r.color.tex)
	for key, t := range r.images {
		release(&t.tex)
		delete(r.images, key)
	}
	release(&r.instBuf)
	r.instCap = 0
}

// changedSince returns what changed between the frame drawn in memory the
// drawable of IOSurface key holds and frame n: everything when it holds
// none or one too old.
func (r *Renderer) changedSince(key uint32, n uint64, width, height int) []image.Rectangle {
	shown, ok := r.shown[key]
	if !ok || n-shown > pixelHistory {
		return []image.Rectangle{image.Rect(0, 0, width, height)}
	}
	var rects []image.Rectangle
	for k := shown + 1; k <= n; k++ {
		rects = append(rects, r.pixelDamage[k%pixelHistory]...)
	}
	return rects
}

// copyRect copies rc of src, rows of srcStride bytes of 4-byte pixels,
// into dst, rows of dstStride bytes. Both hold rc.
func copyRect(dst []byte, dstStride int, src []byte, srcStride int, rc image.Rectangle) {
	for y := rc.Min.Y; y < rc.Max.Y; y++ {
		copy(dst[y*dstStride+rc.Min.X*4:y*dstStride+rc.Max.X*4], src[y*srcStride+rc.Min.X*4:])
	}
}

func surfaceID(surface id) uint32 {
	n, _, _ := purego.SyscallN(ioSurfaceID, surface)
	return uint32(n)
}

// trimAfter is how long a window draws nothing before its spare drawables
// go: about when the Metal driver frees its own memory of frames.
const trimAfter = 2 * time.Second

// armTrim has the renderer's timer trim the drawables once it draws
// nothing for trimAfter.
func (r *Renderer) armTrim() {
	if r.trimArmed {
		return
	}
	if r.trimTimer == 0 {
		lastTrimHandle++
		r.trimHandle = lastTrimHandle
		ctx := cfTimerContext{info: r.trimHandle}
		// Its first fire date comes below; it repeats only in theory.
		r.trimTimer = cfRunLoopTimerCreate(0, cfAbsoluteTimeGetCurrent()+1e9, 1e9, 0, 0, trimCallback, &ctx)
		if r.trimTimer == 0 {
			return
		}
		trimTimers[r.trimHandle] = r
		mainLoop, _, _ := purego.SyscallN(cfRunLoopGetMain)
		purego.SyscallN(cfRunLoopAddTimer, mainLoop, r.trimTimer, commonModes)
	}
	cfRunLoopTimerSetNextFireDate(r.trimTimer, cfAbsoluteTimeGetCurrent()+trimAfter.Seconds())
	r.trimArmed = true
}

// trimIfIdle trims the drawables if the window drew nothing for trimAfter,
// and waits for that time otherwise.
func (r *Renderer) trimIfIdle() {
	if wait := trimAfter - time.Since(r.lastRender); wait > 0 {
		cfRunLoopTimerSetNextFireDate(r.trimTimer, cfAbsoluteTimeGetCurrent()+wait.Seconds())
		return
	}
	r.trimArmed = false
	cfRunLoopTimerSetNextFireDate(r.trimTimer, cfAbsoluteTimeGetCurrent()+1e9)
	r.trim()
}

// trim gives back the drawables the layer does not show. Shrinking them
// frees those, and Core Animation keeps showing the last frame; the next
// makes them again at full size.
func (r *Renderer) trim() {
	if r.layer == 0 || r.trimmed {
		return
	}
	pool(func() {
		tx := class("CATransaction")
		send(tx, "begin")
		send(tx, "setDisableActions:", 1)
		msgSetSize(r.layer, sel("setDrawableSize:"), cgSize{1, 1})
		send(tx, "commit")
	})
	r.trimmed = true
	clear(r.shown)
}

// renderOffscreen draws s into a texture of its own and returns its
// premultiplied BGRA rows, for tests.
func (r *Renderer) renderOffscreen(s *scene.Scene) (pix []byte, err error) {
	pool(func() {
		r.waitLast()
		tex := r.newTexture(s.Width, s.Height, pixelFormatBGRA8Unorm, usageRenderTarget|usageShaderRead, nil, 0)
		if tex == 0 {
			err = errors.New("metal: cannot create a target")
			return
		}
		defer release(&tex)
		var cb id
		if cb, err = r.encode(s, tex); err != nil {
			return
		}
		blit := send(cb, "blitCommandEncoder")
		send(blit, "synchronizeResource:", tex)
		send(blit, "endEncoding")
		send(cb, "commit")
		send(cb, "waitUntilCompleted")
		if int(send(cb, "status")) == statusError {
			err = fmt.Errorf("metal: the frame failed: %s", describe(send(cb, "error")))
			return
		}
		pix = make([]byte, s.Width*s.Height*4)
		msgGetBytes(tex, sel("getBytes:bytesPerRow:fromRegion:mipmapLevel:"), unsafe.Pointer(&pix[0]), uint(s.Width*4),
			mtlRegion{W: uint(s.Width), H: uint(s.Height), D: 1}, 0)
	})
	return pix, err
}

// Release frees the renderer's GPU objects and takes its layer out.
func (r *Renderer) Release() {
	if r.trimTimer != 0 {
		purego.SyscallN(cfRunLoopTimerInvalidate, r.trimTimer)
		purego.SyscallN(cfRelease, r.trimTimer)
		delete(trimTimers, r.trimHandle)
		r.trimTimer = 0
	}
	pool(func() {
		r.waitLast()
		if r.layer != 0 {
			tx := class("CATransaction")
			send(tx, "begin")
			send(tx, "setDisableActions:", 1)
			send(r.layer, "removeFromSuperlayer")
			send(tx, "commit")
			release(&r.layer)
		}
		r.releaseTextures()
		release(&r.empty)
		release(&r.sampler)
		release(&r.pipeline)
		release(&r.queue)
		release(&r.device)
	})
}
