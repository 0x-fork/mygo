package vt

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"unsafe"
)

// Data of the Kitty graphics protocol's images (GhosttyKittyGraphicsData,
// GhosttyKittyGraphicsImageData, GhosttyKittyGraphicsPlacementData).
const (
	dataKittyGraphics = 30

	kittyPlacementIterator = 1

	imageWidth      = 3
	imageHeight     = 4
	imageFormat     = 5
	imageData       = 7
	imageDataLen    = 8
	imageGeneration = 9

	placementImageID = 1
	placementID      = 2
	placementVirtual = 3
	placementXOffset = 4
	placementYOffset = 5
	placementColumns = 10
	placementRows    = 11
	placementZ       = 12
	placementSourceX = 6
	placementSourceY = 7
	placementSourceW = 8
	placementSourceH = 9
)

// ImageFormat is how an image's pixels are laid out (GhosttyKittyImageFormat).
type ImageFormat int32

// Image formats: PNG is decoded to RGBA as an image arrives.
const (
	ImageRGB       ImageFormat = 0
	ImageRGBA      ImageFormat = 1
	ImageGrayAlpha ImageFormat = 3
	ImageGray      ImageFormat = 4
)

// SetImageLimit sets how many bytes the images of the Kitty graphics
// protocol may take, zero refusing them (and deleting those there are).
func (t *Terminal) SetImageLimit(bytes uint64) {
	t.set(optKittyImageLimit, unsafe.Pointer(&bytes))
}

// Image is an image of the Kitty graphics protocol.
type Image struct {
	ID            uint32
	Width, Height int
	Format        ImageFormat
	// Pixels are the image's, decoded; Generation changes when they do.
	Pixels     []byte
	Generation uint64
}

// Placement is where a program placed an image (Kitty graphics), as the
// screen shows it now.
type Placement struct {
	ImageID, ID uint32
	// Virtual tells that the placement shows through the cells of Unicode
	// placeholders rather than where it is placed.
	Virtual bool
	// Col and Row are its top left cell on the screen, Row negative when
	// its top scrolled off; Visible tells that some of it shows.
	Col, Row int
	Visible  bool
	// XOffset and YOffset place it within its first cell, in pixels;
	// Width and Height are the pixels it takes on the screen, Cols and Rows
	// the cells.
	XOffset, YOffset int
	Width, Height    int
	Cols, Rows       int
	// SrcX, SrcY, SrcW and SrcH are the part of the image it shows, in the
	// image's pixels.
	SrcX, SrcY, SrcW, SrcH int
	// Z orders it: below the cells' backgrounds when below -1<<30, below
	// the text when negative, above it otherwise.
	Z int32
	// Params are the placement as a program asked for it: offsets, the
	// part of the image and the cells, 0 for what the image gives.
	ParamCols, ParamRows                       int
	ParamSrcX, ParamSrcY, ParamSrcW, ParamSrcH int
}

// renderInfo is a GhosttyKittyGraphicsPlacementRenderInfo.
type renderInfo struct {
	size                                        uintptr
	pixelWidth, pixelHeight, gridCols, gridRows uint32
	viewportCol, viewportRow                    int32
	viewportVisible                             bool
	sourceX, sourceY, sourceW, sourceH          uint32
}

// graphics returns the image storage of the screen that shows, 0 without
// one.
func (t *Terminal) graphics() uintptr {
	var g uintptr
	if t.get(dataKittyGraphics, unsafe.Pointer(&g)) != nil {
		return 0
	}
	return g
}

// Placements calls fn with each placement of the screen that shows.
func (t *Terminal) Placements(fn func(Placement)) {
	g := t.graphics()
	if g == 0 {
		return
	}
	var it uintptr
	if !ok(call(fnPlacementIteratorNew, 0, uintptr(unsafe.Pointer(&it)))) || it == 0 {
		return
	}
	defer call(fnPlacementIteratorFree, it)
	if !ok(call(fnKittyGet, g, kittyPlacementIterator, uintptr(unsafe.Pointer(&it)))) {
		return
	}
	u32 := func(data int) int {
		var v uint32
		call(fnPlacementGet, it, uintptr(data), uintptr(unsafe.Pointer(&v)))
		return int(v)
	}
	for cbool(call(fnPlacementNext, it)) {
		var p Placement
		p.ImageID, p.ID = uint32(u32(placementImageID)), uint32(u32(placementID))
		call(fnPlacementGet, it, placementVirtual, uintptr(unsafe.Pointer(&p.Virtual)))
		call(fnPlacementGet, it, placementZ, uintptr(unsafe.Pointer(&p.Z)))
		p.XOffset, p.YOffset = u32(placementXOffset), u32(placementYOffset)
		p.ParamCols, p.ParamRows = u32(placementColumns), u32(placementRows)
		p.ParamSrcX, p.ParamSrcY = u32(placementSourceX), u32(placementSourceY)
		p.ParamSrcW, p.ParamSrcH = u32(placementSourceW), u32(placementSourceH)
		img := call(fnKittyImage, g, uintptr(p.ImageID))
		if img != 0 {
			ri := renderInfo{size: unsafe.Sizeof(renderInfo{})}
			if ok(call(fnPlacementRenderInfo, it, img, t.h, uintptr(unsafe.Pointer(&ri)))) {
				p.Col, p.Row, p.Visible = int(ri.viewportCol), int(ri.viewportRow), ri.viewportVisible
				p.Width, p.Height = int(ri.pixelWidth), int(ri.pixelHeight)
				p.Cols, p.Rows = int(ri.gridCols), int(ri.gridRows)
				p.SrcX, p.SrcY, p.SrcW, p.SrcH = int(ri.sourceX), int(ri.sourceY), int(ri.sourceW), int(ri.sourceH)
			}
		}
		fn(p)
	}
}

// ImageGeneration returns the generation of an image, 0 when there is no
// such image or its pixels have yet to arrive.
func (t *Terminal) ImageGeneration(id uint32) uint64 {
	img := t.image(id)
	if img == 0 {
		return 0
	}
	var gen uint64
	var p uintptr
	if !ok(call(fnKittyImageGet, img, imageData, uintptr(unsafe.Pointer(&p)))) || p == 0 {
		return 0
	}
	call(fnKittyImageGet, img, imageGeneration, uintptr(unsafe.Pointer(&gen)))
	return gen
}

func (t *Terminal) image(id uint32) uintptr {
	g := t.graphics()
	if g == 0 {
		return 0
	}
	return call(fnKittyImage, g, uintptr(id))
}

// Image returns an image, its pixels copied; false when there is no such
// image or its pixels have yet to arrive.
func (t *Terminal) Image(id uint32) (Image, bool) {
	img := t.image(id)
	if img == 0 {
		return Image{}, false
	}
	var w, h uint32
	var format int32
	var p, n uintptr
	var gen uint64
	call(fnKittyImageGet, img, imageWidth, uintptr(unsafe.Pointer(&w)))
	call(fnKittyImageGet, img, imageHeight, uintptr(unsafe.Pointer(&h)))
	call(fnKittyImageGet, img, imageFormat, uintptr(unsafe.Pointer(&format)))
	call(fnKittyImageGet, img, imageGeneration, uintptr(unsafe.Pointer(&gen)))
	if !ok(call(fnKittyImageGet, img, imageData, uintptr(unsafe.Pointer(&p)))) || p == 0 {
		return Image{}, false
	}
	call(fnKittyImageGet, img, imageDataLen, uintptr(unsafe.Pointer(&n)))
	pixels := make([]byte, n)
	copy(pixels, unsafe.Slice((*byte)(mem(p)), n))
	return Image{ID: id, Width: int(w), Height: int(h), Format: ImageFormat(format), Pixels: pixels, Generation: gen}, true
}

// sysDecodePNG is GHOSTTY_SYS_OPT_DECODE_PNG.
const sysDecodePNG = 1

// sysImage is a GhosttySysImage: a PNG decoded, its pixels RGBA in memory
// of the library's allocator.
type sysImage struct {
	width, height uint32
	data, dataLen uintptr
}

// decodePNG gives the library a PNG decoder, without which it refuses
// images sent as PNG (Kitty graphics, f=100), as grok sends them.
func decodePNG() {
	cb := newCallback(func(_, alloc, data, n, out uintptr) uintptr {
		if data == 0 || n == 0 {
			return 0
		}
		img, err := png.Decode(bytes.NewReader(unsafe.Slice((*byte)(mem(data)), n)))
		if err != nil {
			return 0
		}
		b := img.Bounds()
		rgba, ok := img.(*image.NRGBA)
		if !ok || rgba.Stride != 4*b.Dx() || b.Min != (image.Point{}) {
			rgba = image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
			draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
		}
		size := uintptr(len(rgba.Pix))
		p := call(fnAlloc, alloc, size)
		if p == 0 {
			return 0
		}
		copy(unsafe.Slice((*byte)(mem(p)), size), rgba.Pix)
		*(*sysImage)(mem(out)) = sysImage{width: uint32(b.Dx()), height: uint32(b.Dy()), data: p, dataLen: size}
		return 1
	})
	call(fnSysSet, sysDecodePNG, cb)
}
