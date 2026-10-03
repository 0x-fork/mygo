//go:build darwin

package text

import (
	"errors"
	"fmt"
	"log"
	"math"
	"runtime"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Core Text lays out paragraphs: a typesetter finds the fonts, falls back
// to others for what a font lacks, shapes, and suggests line breaks, and
// Core Graphics rasterizes glyphs, in color for color fonts such as Apple
// Color Emoji, and the others with font smoothing, as AppKit draws text.
// The system font comes from NSFont, which knows its weights.

func newEngine() engine {
	e, err := newCoreText()
	if err != nil {
		log.Printf("mygo: %v: text will not show", err)
		return &stubEngine{}
	}
	return e
}

// Core Foundation, Core Text and Core Graphics structs passed by value.
type (
	cfRange struct{ location, length int }
	cgPoint struct{ x, y float64 }
	cgSize  struct{ w, h float64 }
	cgRect  struct{ x, y, w, h float64 }

	ctParagraphStyleSetting struct {
		spec  uint32
		size  uintptr
		value unsafe.Pointer
	}
)

const (
	cfStringEncodingUTF8 = 0x08000100
	cfNumberSInt32Type   = 3
	cfNumberSInt64Type   = 4
	cfNumberFloat64Type  = 6

	ctFontUIFontSystem         = 2
	ctFontItalicTrait          = 1 << 0
	ctFontColorGlyphsTrait     = 1 << 13
	ctFontOrientationDefault   = 0
	ctRunStatusRightToLeft     = 1 << 0
	ctBaseWritingDirectionSpec = 13

	cgImageAlphaOnly             = 7
	cgImageAlphaPremultipliedLst = 1
	cgBitmapByteOrder32Big       = 4 << 12
)

var ct struct {
	// Core Foundation
	release               func(obj uintptr)
	retain                func(obj uintptr) uintptr
	hash                  func(obj uintptr) uint
	equal                 func(a, b uintptr) bool
	stringWithCharacters  func(alloc uintptr, chars *uint16, n int) uintptr
	stringWithBytes       func(alloc uintptr, bytes *byte, n int, encoding uint32, external bool) uintptr
	stringGetLength       func(s uintptr) int
	stringGetCharacters   func(s uintptr, r cfRange, out *uint16)
	dictionaryCreate      func(alloc uintptr, keys, values *uintptr, n int, keyCallbacks, valueCallbacks uintptr) uintptr
	dictionaryGetValue    func(d, key uintptr) uintptr
	arrayGetCount         func(a uintptr) int
	arrayGetValueAtIndex  func(a uintptr, i int) uintptr
	setCreate             func(alloc uintptr, values *uintptr, n int, callbacks uintptr) uintptr
	arrayCreate           func(alloc uintptr, values *uintptr, n int, callbacks uintptr) uintptr
	numberCreate          func(alloc uintptr, typ int, value unsafe.Pointer) uintptr
	numberGetValue        func(n uintptr, typ int, value unsafe.Pointer) bool
	attributedString      func(alloc, str, attrs uintptr) uintptr
	attributedMutable     func(alloc uintptr, max int, attributed uintptr) uintptr
	attributedSet         func(attributed uintptr, r cfRange, name, value uintptr)
	dataCreate            func(alloc uintptr, bytes *byte, n int) uintptr
	keyCallbacks          uintptr
	valueCallbacks        uintptr
	setCallbacks          uintptr
	arrayCallbacks        uintptr
	fontAttributeName     uintptr
	paragraphStyleName    uintptr
	familyNameAttribute   uintptr
	traitsAttribute       uintptr
	weightTrait           uintptr
	symbolicTrait         uintptr
	srgbName              uintptr
	fontWithDescriptor    func(desc uintptr, size float64, matrix uintptr) uintptr
	uiFontForLanguage     func(typ uint32, size float64, language uintptr) uintptr
	fontWithTraits        func(font uintptr, size float64, matrix uintptr, value, mask uint32) uintptr
	fontWithAttrs         func(font uintptr, size float64, matrix, desc uintptr) uintptr
	descriptorWithAttrs   func(attrs uintptr) uintptr
	descriptorMatching    func(desc, mandatory uintptr) uintptr
	descriptorAttribute   func(desc, name uintptr) uintptr
	descriptorsFromData   func(data uintptr) uintptr
	fontGetAscent         func(font uintptr) float64
	fontGetDescent        func(font uintptr) float64
	fontGetLeading        func(font uintptr) float64
	fontGetSize           func(font uintptr) float64
	fontGetSymbolicTraits func(font uintptr) uint32
	fontGetBoundingRects  func(font uintptr, orientation uint32, glyphs *uint16, rects *cgRect, n int) cgRect
	fontDrawGlyphs        func(font uintptr, glyphs *uint16, positions *cgPoint, n int, context uintptr)
	paragraphStyleCreate  func(settings *ctParagraphStyleSetting, n int) uintptr
	typesetterCreate      func(str uintptr) uintptr
	suggestLineBreak      func(typesetter uintptr, start int, width float64) int
	typesetterCreateLine  func(typesetter uintptr, r cfRange) uintptr
	lineGetGlyphRuns      func(line uintptr) uintptr
	lineGetStringRange    func(line uintptr) cfRange
	runGetGlyphCount      func(run uintptr) int
	runGetStringRange     func(run uintptr) cfRange
	runGetStatus          func(run uintptr) uint32
	runGetAttributes      func(run uintptr) uintptr
	runGetGlyphs          func(run uintptr, r cfRange, out *uint16)
	runGetPositions       func(run uintptr, r cfRange, out *cgPoint)
	runGetAdvances        func(run uintptr, r cfRange, out *cgSize)
	runGetStringIndices   func(run uintptr, r cfRange, out *int)

	// Optional: tracking (macOS 10.12) and OpenType features by tag
	// (macOS 10.13).
	trackingName    uintptr
	kernName        uintptr
	cascadeList     uintptr
	featureSettings uintptr
	featureTag      uintptr
	featureValue    uintptr

	// Core Graphics
	colorSpaceWithName      func(name uintptr) uintptr
	colorSpaceDeviceRGB     func() uintptr
	bitmapContextCreate     func(data unsafe.Pointer, w, h, bitsPerComponent, bytesPerRow int, space uintptr, info uint32) uintptr
	contextRelease          func(ctx uintptr)
	contextAntialias        func(ctx uintptr, on bool)
	contextSmoothFonts      func(ctx uintptr, on bool)
	contextAllowSubpixelPos func(ctx uintptr, on bool)
	contextSubpixelPos      func(ctx uintptr, on bool)
	contextAllowQuantize    func(ctx uintptr, on bool)
	contextQuantize         func(ctx uintptr, on bool)
	contextScaleCTM         func(ctx uintptr, sx, sy float64)
	contextSetFill          func(ctx uintptr, r, g, b, a float64)

	// Objective-C
	poolPush func() uintptr
	poolPop  func(pool uintptr)
}

func loadCoreText() error {
	var missing []string
	lib := func(path string) uintptr {
		h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			missing = append(missing, path)
		}
		return h
	}
	bind := func(lib uintptr, fn any, name string) {
		sym, err := purego.Dlsym(lib, name)
		if err != nil || sym == 0 {
			missing = append(missing, name)
			return
		}
		purego.RegisterFunc(fn, sym)
	}
	// Constants are pointers to CFStringRefs, or callback structs.
	addr := func(lib uintptr, name string) uintptr {
		sym, err := purego.Dlsym(lib, name)
		if err != nil || sym == 0 {
			missing = append(missing, name)
		}
		return sym
	}
	value := func(lib uintptr, name string) uintptr {
		p := addr(lib, name)
		if p == 0 {
			return 0
		}
		return **(**uintptr)(unsafe.Pointer(&p))
	}
	cf := lib("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
	text := lib("/System/Library/Frameworks/CoreText.framework/CoreText")
	cg := lib("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
	objcLib := lib("/usr/lib/libobjc.A.dylib")
	lib("/System/Library/Frameworks/AppKit.framework/AppKit") // NSFont
	if len(missing) > 0 {
		return fmt.Errorf("cannot load %s", strings.Join(missing, ", "))
	}
	bind(cf, &ct.release, "CFRelease")
	bind(cf, &ct.retain, "CFRetain")
	bind(cf, &ct.hash, "CFHash")
	bind(cf, &ct.equal, "CFEqual")
	bind(cf, &ct.stringWithCharacters, "CFStringCreateWithCharacters")
	bind(cf, &ct.stringWithBytes, "CFStringCreateWithBytes")
	bind(cf, &ct.stringGetLength, "CFStringGetLength")
	bind(cf, &ct.stringGetCharacters, "CFStringGetCharacters")
	bind(cf, &ct.dictionaryCreate, "CFDictionaryCreate")
	bind(cf, &ct.dictionaryGetValue, "CFDictionaryGetValue")
	bind(cf, &ct.arrayGetCount, "CFArrayGetCount")
	bind(cf, &ct.arrayGetValueAtIndex, "CFArrayGetValueAtIndex")
	bind(cf, &ct.setCreate, "CFSetCreate")
	bind(cf, &ct.arrayCreate, "CFArrayCreate")
	bind(cf, &ct.numberCreate, "CFNumberCreate")
	bind(cf, &ct.numberGetValue, "CFNumberGetValue")
	bind(cf, &ct.attributedString, "CFAttributedStringCreate")
	bind(cf, &ct.attributedMutable, "CFAttributedStringCreateMutableCopy")
	bind(cf, &ct.attributedSet, "CFAttributedStringSetAttribute")
	bind(cf, &ct.dataCreate, "CFDataCreate")
	ct.keyCallbacks = addr(cf, "kCFTypeDictionaryKeyCallBacks")
	ct.valueCallbacks = addr(cf, "kCFTypeDictionaryValueCallBacks")
	ct.setCallbacks = addr(cf, "kCFTypeSetCallBacks")
	ct.arrayCallbacks = addr(cf, "kCFTypeArrayCallBacks")
	ct.fontAttributeName = value(text, "kCTFontAttributeName")
	ct.paragraphStyleName = value(text, "kCTParagraphStyleAttributeName")
	ct.familyNameAttribute = value(text, "kCTFontFamilyNameAttribute")
	ct.traitsAttribute = value(text, "kCTFontTraitsAttribute")
	ct.weightTrait = value(text, "kCTFontWeightTrait")
	ct.symbolicTrait = value(text, "kCTFontSymbolicTrait")
	bind(text, &ct.fontWithDescriptor, "CTFontCreateWithFontDescriptor")
	bind(text, &ct.uiFontForLanguage, "CTFontCreateUIFontForLanguage")
	bind(text, &ct.fontWithTraits, "CTFontCreateCopyWithSymbolicTraits")
	bind(text, &ct.fontWithAttrs, "CTFontCreateCopyWithAttributes")
	bind(text, &ct.descriptorWithAttrs, "CTFontDescriptorCreateWithAttributes")
	bind(text, &ct.descriptorMatching, "CTFontDescriptorCreateMatchingFontDescriptor")
	bind(text, &ct.descriptorAttribute, "CTFontDescriptorCopyAttribute")
	bind(text, &ct.fontGetAscent, "CTFontGetAscent")
	bind(text, &ct.fontGetDescent, "CTFontGetDescent")
	bind(text, &ct.fontGetLeading, "CTFontGetLeading")
	bind(text, &ct.fontGetSize, "CTFontGetSize")
	bind(text, &ct.fontGetSymbolicTraits, "CTFontGetSymbolicTraits")
	bind(text, &ct.fontGetBoundingRects, "CTFontGetBoundingRectsForGlyphs")
	bind(text, &ct.fontDrawGlyphs, "CTFontDrawGlyphs")
	bind(text, &ct.paragraphStyleCreate, "CTParagraphStyleCreate")
	bind(text, &ct.typesetterCreate, "CTTypesetterCreateWithAttributedString")
	bind(text, &ct.suggestLineBreak, "CTTypesetterSuggestLineBreak")
	bind(text, &ct.typesetterCreateLine, "CTTypesetterCreateLine")
	bind(text, &ct.lineGetGlyphRuns, "CTLineGetGlyphRuns")
	bind(text, &ct.lineGetStringRange, "CTLineGetStringRange")
	bind(text, &ct.runGetGlyphCount, "CTRunGetGlyphCount")
	bind(text, &ct.runGetStringRange, "CTRunGetStringRange")
	bind(text, &ct.runGetStatus, "CTRunGetStatus")
	bind(text, &ct.runGetAttributes, "CTRunGetAttributes")
	bind(text, &ct.runGetGlyphs, "CTRunGetGlyphs")
	bind(text, &ct.runGetPositions, "CTRunGetPositions")
	bind(text, &ct.runGetAdvances, "CTRunGetAdvances")
	bind(text, &ct.runGetStringIndices, "CTRunGetStringIndices")
	bind(cg, &ct.colorSpaceDeviceRGB, "CGColorSpaceCreateDeviceRGB")
	bind(cg, &ct.bitmapContextCreate, "CGBitmapContextCreate")
	bind(cg, &ct.contextRelease, "CGContextRelease")
	bind(cg, &ct.contextAntialias, "CGContextSetShouldAntialias")
	bind(cg, &ct.contextSmoothFonts, "CGContextSetShouldSmoothFonts")
	bind(cg, &ct.contextAllowSubpixelPos, "CGContextSetAllowsFontSubpixelPositioning")
	bind(cg, &ct.contextSubpixelPos, "CGContextSetShouldSubpixelPositionFonts")
	bind(cg, &ct.contextAllowQuantize, "CGContextSetAllowsFontSubpixelQuantization")
	bind(cg, &ct.contextQuantize, "CGContextSetShouldSubpixelQuantizeFonts")
	bind(cg, &ct.contextScaleCTM, "CGContextScaleCTM")
	bind(cg, &ct.contextSetFill, "CGContextSetRGBFillColor")
	bind(objcLib, &ct.poolPush, "objc_autoreleasePoolPush")
	bind(objcLib, &ct.poolPop, "objc_autoreleasePoolPop")
	if len(missing) > 0 {
		return fmt.Errorf("missing %s", strings.Join(missing, ", "))
	}
	optional := func(lib uintptr, name string) uintptr {
		if p, err := purego.Dlsym(lib, name); err == nil && p != 0 {
			return **(**uintptr)(unsafe.Pointer(&p))
		}
		return 0
	}
	ct.trackingName = optional(text, "kCTTrackingAttributeName")
	ct.kernName = optional(text, "kCTKernAttributeName")
	ct.cascadeList = optional(text, "kCTFontCascadeListAttribute")
	ct.featureSettings = optional(text, "kCTFontFeatureSettingsAttribute")
	ct.featureTag = optional(text, "kCTFontOpenTypeFeatureTag")
	ct.featureValue = optional(text, "kCTFontOpenTypeFeatureValue")
	// Optional: adding fonts from memory (macOS 10.13), sRGB by name.
	if sym, err := purego.Dlsym(text, "CTFontManagerCreateFontDescriptorsFromData"); err == nil && sym != 0 {
		purego.RegisterFunc(&ct.descriptorsFromData, sym)
	}
	if sym, err := purego.Dlsym(cg, "CGColorSpaceCreateWithName"); err == nil && sym != 0 {
		purego.RegisterFunc(&ct.colorSpaceWithName, sym)
		if p, err := purego.Dlsym(cg, "kCGColorSpaceSRGB"); err == nil && p != 0 {
			ct.srgbName = **(**uintptr)(unsafe.Pointer(&p))
		}
	}
	return nil
}

type coreText struct {
	styles [2]uintptr // paragraph styles: left-to-right, right-to-left
	srgb   uintptr

	primary map[Style]uintptr // the CTFont of each style
	fonts   map[uint][]*Font  // by CFHash of their CTFont
	color   map[uintptr]bool

	registered map[string][]registeredFace // by lowercased family
}

type registeredFace struct {
	desc   uintptr
	weight float64 // -1 to 1
	italic bool
}

func newCoreText() (*coreText, error) {
	if err := loadCoreText(); err != nil {
		return nil, err
	}
	e := &coreText{
		primary:    map[Style]uintptr{},
		fonts:      map[uint][]*Font{},
		color:      map[uintptr]bool{},
		registered: map[string][]registeredFace{},
	}
	for i, dir := range [2]int8{0, 1} { // kCTWritingDirectionLeftToRight, RightToLeft
		d := dir
		s := ctParagraphStyleSetting{spec: ctBaseWritingDirectionSpec, size: 1, value: unsafe.Pointer(&d)}
		e.styles[i] = ct.paragraphStyleCreate(&s, 1)
		runtime.KeepAlive(&d)
	}
	if ct.colorSpaceWithName != nil && ct.srgbName != 0 {
		e.srgb = ct.colorSpaceWithName(ct.srgbName)
	}
	if e.srgb == 0 {
		e.srgb = ct.colorSpaceDeviceRGB()
	}
	return e, nil
}

func cfString(s string) uintptr {
	if s == "" {
		return ct.stringWithBytes(0, nil, 0, cfStringEncodingUTF8, false)
	}
	b := []byte(s)
	return ct.stringWithBytes(0, &b[0], len(b), cfStringEncodingUTF8, false)
}

func goString(s uintptr) string {
	n := ct.stringGetLength(s)
	if n <= 0 {
		return ""
	}
	buf := make([]uint16, n)
	ct.stringGetCharacters(s, cfRange{0, n}, &buf[0])
	return string(utf16Decode(buf))
}

func utf16Decode(u []uint16) []rune {
	out := make([]rune, 0, len(u))
	for i := 0; i < len(u); i++ {
		r := rune(u[i])
		if r >= 0xD800 && r < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000 {
			r = (r-0xD800)<<10 + rune(u[i+1]) - 0xDC00 + 0x10000
			i++
		}
		out = append(out, r)
	}
	return out
}

func cfDictionary(keys, values []uintptr) uintptr {
	return ct.dictionaryCreate(0, &keys[0], &values[0], len(keys), ct.keyCallbacks, ct.valueCallbacks)
}

func cfFloat(v float64) uintptr { return ct.numberCreate(0, cfNumberFloat64Type, unsafe.Pointer(&v)) }

// nsWeight converts a CSS weight to NSFont's (and Core Text's) scale, from
// -1 to 1.
func nsWeight(w int) float64 {
	steps := [...]float64{-0.8, -0.6, -0.4, 0, 0.23, 0.3, 0.4, 0.56, 0.62}
	f := (float64(max(100, min(w, 900))) - 100) / 100
	i := min(int(f), len(steps)-2)
	return steps[i] + (steps[i+1]-steps[i])*(f-float64(i))
}

// systemFont returns the system font, or its monospaced kind, owned.
func (e *coreText) systemFont(size float32, weight int, italic, mono bool) uintptr {
	// An autorelease pool belongs to a thread, which the goroutine must
	// not leave before popping it, or Objective-C crashes.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := ct.poolPush()
	defer ct.poolPop(pool)
	sel := "systemFontOfSize:weight:"
	if mono {
		sel = "monospacedSystemFontOfSize:weight:"
	}
	var font uintptr
	if cls := objc.ID(objc.GetClass("NSFont")); cls != 0 && objc.Send[bool](cls, objc.RegisterName("respondsToSelector:"), objc.RegisterName(sel)) {
		if font = uintptr(cls.Send(objc.RegisterName(sel), float64(size), nsWeight(weight))); font != 0 {
			ct.retain(font)
		}
	}
	if font == 0 {
		if mono {
			font = e.named("Menlo", size, weight, false)
		} else {
			font = ct.uiFontForLanguage(ctFontUIFontSystem, float64(size), 0)
		}
	}
	if font != 0 && italic {
		if f := ct.fontWithTraits(font, 0, 0, ctFontItalicTrait, ctFontItalicTrait); f != 0 {
			ct.release(font)
			font = f
		}
	}
	return font
}

// named returns a font of a family the system has, owned, or 0.
func (e *coreText) named(family string, size float32, weight int, italic bool) uintptr {
	desc := e.namedDesc(family, weight, italic)
	if desc == 0 {
		return 0
	}
	defer ct.release(desc)
	return ct.fontWithDescriptor(desc, float64(size), 0)
}

// namedDesc returns the descriptor of the face of a family the system has
// that best matches a weight and italics, owned, or 0.
func (e *coreText) namedDesc(family string, weight int, italic bool) uintptr {
	name := cfString(family)
	defer ct.release(name)
	symbolic := int32(0)
	if italic {
		symbolic = ctFontItalicTrait
	}
	w := cfFloat(nsWeight(weight))
	defer ct.release(w)
	s := ct.numberCreate(0, cfNumberSInt32Type, unsafe.Pointer(&symbolic))
	defer ct.release(s)
	traits := cfDictionary([]uintptr{ct.weightTrait, ct.symbolicTrait}, []uintptr{w, s})
	defer ct.release(traits)
	attrs := cfDictionary([]uintptr{ct.familyNameAttribute, ct.traitsAttribute}, []uintptr{name, traits})
	defer ct.release(attrs)
	desc := ct.descriptorWithAttrs(attrs)
	if desc == 0 {
		return 0
	}
	defer ct.release(desc)
	mandatory := []uintptr{ct.familyNameAttribute}
	set := ct.setCreate(0, &mandatory[0], 1, ct.setCallbacks)
	defer ct.release(set)
	return ct.descriptorMatching(desc, set)
}

// ctFont returns the CTFont of a style.
func (e *coreText) ctFont(style Style) uintptr {
	key := Style{Family: style.Family, Size: style.FontSize(), Weight: style.weight(), Italic: style.Italic, Features: style.Features}
	if f, ok := e.primary[key]; ok {
		return f
	}
	if len(e.primary) >= 256 {
		for _, f := range e.primary {
			ct.release(f)
		}
		clear(e.primary)
	}
	if key.Features != "" {
		// The font without the features, with them.
		base := key
		base.Features = ""
		font := e.ctFont(base)
		if with := withFeatures(font, features(key.Features)); with != 0 {
			font = with
		} else {
			ct.retain(font)
		}
		e.primary[key] = font
		return font
	}
	var font uintptr
	families, chosen := familyList(key.Family), -1
	for i, family := range families {
		chosen = i
		switch generic(family) {
		case "system-ui", "sans-serif":
			font = e.systemFont(key.Size, key.Weight, key.Italic, false)
		case "monospace":
			font = e.systemFont(key.Size, key.Weight, key.Italic, true)
		case "serif":
			for _, name := range []string{"Times New Roman", "Times", "Georgia"} {
				if font = e.named(name, key.Size, key.Weight, key.Italic); font != 0 {
					break
				}
			}
		default:
			if faces, ok := e.registered[strings.ToLower(family)]; ok {
				font = registeredFont(faces, key)
			} else {
				font = e.named(family, key.Size, key.Weight, key.Italic)
			}
		}
		if font != 0 {
			break
		}
	}
	if font == 0 {
		font = e.systemFont(key.Size, key.Weight, key.Italic, false)
	} else if ct.cascadeList != 0 {
		// The families after it come before the system's for what it
		// lacks.
		var descs []uintptr
		for _, family := range families[chosen+1:] {
			if generic(family) != "" {
				continue
			}
			if faces, ok := e.registered[strings.ToLower(family)]; ok {
				d := registeredDesc(faces, key)
				ct.retain(d)
				descs = append(descs, d)
			} else if d := e.namedDesc(family, key.Weight, key.Italic); d != 0 {
				descs = append(descs, d)
			}
		}
		if len(descs) > 0 {
			array := ct.arrayCreate(0, &descs[0], len(descs), ct.arrayCallbacks)
			attrs := cfDictionary([]uintptr{ct.cascadeList}, []uintptr{array})
			desc := ct.descriptorWithAttrs(attrs)
			if with := ct.fontWithAttrs(font, 0, 0, desc); with != 0 {
				ct.release(font)
				font = with
			}
			for _, obj := range append(descs, array, attrs, desc) {
				ct.release(obj)
			}
		}
	}
	e.primary[key] = font
	return font
}

// styleSpans returns a copy of the attributed string of text, of style,
// with the font and tracking of each span over its runes, owned.
func (e *coreText) styleSpans(attributed uintptr, style Style, spans []Span, text []rune) uintptr {
	styled := ct.attributedMutable(0, 0, attributed)
	// The UTF-16 code unit of each rune.
	at := make([]int, len(text)+1)
	for i, r := range text {
		at[i+1] = at[i] + 1
		if r >= 0x10000 {
			at[i+1]++
		}
	}
	from := 0
	for _, sp := range spans {
		to := max(from, min(sp.End, len(text)))
		r := cfRange{at[from], at[to] - at[from]}
		from = to
		if r.length == 0 {
			continue
		}
		s := sp.style(style)
		if font := e.ctFont(s); font != 0 {
			ct.attributedSet(styled, r, ct.fontAttributeName, font)
		}
		if s.LetterSpacing != style.LetterSpacing && ct.trackingName != 0 {
			tracking := cfFloat(float64(s.LetterSpacing))
			ct.attributedSet(styled, r, ct.trackingName, tracking)
			ct.release(tracking)
		}
		if noKerning(sp.Features) && ct.kernName != 0 {
			zero := cfFloat(0)
			ct.attributedSet(styled, r, ct.kernName, zero)
			ct.release(zero)
		}
	}
	return styled
}

// noKerning reports whether features turn kerning off, which Core Text
// does by a kern of 0 rather than by the feature.
func noKerning(list string) bool {
	for _, f := range features(list) {
		if f.tag == [4]byte{'k', 'e', 'r', 'n'} && f.value == 0 {
			return true
		}
	}
	return false
}

// withFeatures returns a copy of font with OpenType features, owned, or 0
// when Core Text takes no features by tag.
func withFeatures(font uintptr, fs []feature) uintptr {
	if len(fs) == 0 || ct.featureSettings == 0 || ct.featureTag == 0 || ct.featureValue == 0 {
		return 0
	}
	settings := make([]uintptr, len(fs))
	for i, f := range fs {
		tag := cfString(string(f.tag[:]))
		v := int64(f.value)
		value := ct.numberCreate(0, cfNumberSInt64Type, unsafe.Pointer(&v))
		settings[i] = cfDictionary([]uintptr{ct.featureTag, ct.featureValue}, []uintptr{tag, value})
		ct.release(tag)
		ct.release(value)
	}
	array := ct.arrayCreate(0, &settings[0], len(settings), ct.arrayCallbacks)
	for _, s := range settings {
		ct.release(s)
	}
	attrs := cfDictionary([]uintptr{ct.featureSettings}, []uintptr{array})
	ct.release(array)
	desc := ct.descriptorWithAttrs(attrs)
	ct.release(attrs)
	defer ct.release(desc)
	return ct.fontWithAttrs(font, 0, 0, desc) // size 0 keeps the font's
}

// registeredFont returns the font of the face of a registered family that
// best matches a style, owned.
func registeredFont(faces []registeredFace, style Style) uintptr {
	return ct.fontWithDescriptor(registeredDesc(faces, style), float64(style.FontSize()), 0)
}

// registeredDesc returns the descriptor of the face of a registered family
// that best matches a style, not owned.
func registeredDesc(faces []registeredFace, style Style) uintptr {
	want := nsWeight(style.weight())
	best := -1
	score := func(f registeredFace) float64 {
		s := math.Abs(f.weight - want)
		if f.italic != style.Italic {
			s += 4
		}
		return s
	}
	for i, f := range faces {
		if best < 0 || score(f) < score(faces[best]) {
			best = i
		}
	}
	return faces[best].desc
}

func (e *coreText) font(style Style) *Font {
	f := e.ctFont(style)
	if f == 0 {
		return nil
	}
	return e.fontOf(f)
}

// fontOf returns the Font of a CTFont.
func (e *coreText) fontOf(font uintptr) *Font {
	h := ct.hash(font)
	for _, f := range e.fonts[h] {
		if f.native == font || ct.equal(f.native, font) {
			return f
		}
	}
	ct.retain(font)
	f := &Font{
		Size:    float32(ct.fontGetSize(font)),
		Ascent:  float32(ct.fontGetAscent(font)),
		Descent: float32(ct.fontGetDescent(font)),
		LineGap: float32(ct.fontGetLeading(font)),
		native:  font,
		shaded:  !e.isColor(font),
	}
	e.fonts[h] = append(e.fonts[h], f)
	return f
}

func (e *coreText) shape(text []rune, style Style, spans []Span, width float32, rtl, wholeWords bool) []shapedLine {
	font := e.ctFont(style)
	if font == 0 || len(text) == 0 {
		return nil
	}
	u16, index := utf16Text(text)
	n := len(u16)
	str := ct.stringWithCharacters(0, &u16[0], n)
	defer ct.release(str)
	paragraph := e.styles[0]
	if rtl {
		paragraph = e.styles[1]
	}
	keys, values := []uintptr{ct.fontAttributeName, ct.paragraphStyleName}, []uintptr{font, paragraph}
	if style.LetterSpacing != 0 && ct.trackingName != 0 {
		// Tracking, in points as DIPs, keeps the font's kerning.
		tracking := cfFloat(float64(style.LetterSpacing))
		defer ct.release(tracking)
		keys, values = append(keys, ct.trackingName), append(values, tracking)
	}
	if noKerning(style.Features) && ct.kernName != 0 {
		zero := cfFloat(0)
		defer ct.release(zero)
		keys, values = append(keys, ct.kernName), append(values, zero)
	}
	attrs := cfDictionary(keys, values)
	defer ct.release(attrs)
	attributed := ct.attributedString(0, str, attrs)
	defer ct.release(attributed)
	if len(spans) > 0 {
		styled := e.styleSpans(attributed, style, spans, text)
		defer ct.release(styled)
		attributed = styled
	}
	typesetter := ct.typesetterCreate(attributed)
	if typesetter == 0 {
		return nil
	}
	defer ct.release(typesetter)
	var breaks []bool
	if wholeWords && width > 0 {
		breaks = lineBreaks(text)
	}
	var lines []shapedLine
	for start := 0; start < n; {
		count := n - start
		if width > 0 {
			count = max(ct.suggestLineBreak(typesetter, start, float64(width)), 1)
			// Core Text breaks a word that does not fit a line; carry
			// on to the next break instead.
			if breaks != nil {
				end := min(start+count, n)
				for end < n && (index[end] == index[end-1] || !breaks[index[end]]) {
					end++
				}
				count = end - start
			}
		}
		line := ct.typesetterCreateLine(typesetter, cfRange{start, count})
		if line == 0 {
			break
		}
		lines = append(lines, e.line(line, index, n))
		ct.release(line)
		start += count
	}
	return lines
}

func (e *coreText) line(line uintptr, index []int, n int) shapedLine {
	at := func(i int) int { return index[max(0, min(i, n))] }
	r := ct.lineGetStringRange(line)
	sl := shapedLine{start: at(r.location), end: at(r.location + r.length)}
	runs := ct.lineGetGlyphRuns(line)
	for i := range ct.arrayGetCount(runs) {
		run := ct.arrayGetValueAtIndex(runs, i)
		rr := ct.runGetStringRange(run)
		f := e.fontOf(ct.dictionaryGetValue(ct.runGetAttributes(run), ct.fontAttributeName))
		sr := shapedRun{font: f, start: at(rr.location), end: at(rr.location + rr.length)}
		if count := ct.runGetGlyphCount(run); count > 0 {
			glyphs := make([]uint16, count)
			positions := make([]cgPoint, count)
			advances := make([]cgSize, count)
			indices := make([]int, count)
			all := cfRange{}
			ct.runGetGlyphs(run, all, &glyphs[0])
			ct.runGetPositions(run, all, &positions[0])
			ct.runGetAdvances(run, all, &advances[0])
			ct.runGetStringIndices(run, all, &indices[0])
			rtl := ct.runGetStatus(run)&ctRunStatusRightToLeft != 0
			sr.glyphs = make([]Glyph, count)
			for j := range count {
				sr.glyphs[j] = Glyph{
					Font: f, ID: uint32(glyphs[j]),
					X: float32(positions[j].x), Y: -float32(positions[j].y),
					Advance: float32(advances[j].w),
					Cluster: at(indices[j]), RTL: rtl,
				}
			}
		}
		sl.runs = append(sl.runs, sr)
	}
	return sl
}

func (e *coreText) isColor(font uintptr) bool {
	c, ok := e.color[font]
	if !ok {
		c = ct.fontGetSymbolicTraits(font)&ctFontColorGlyphsTrait != 0
		e.color[font] = c
	}
	return c
}

func (e *coreText) glyph(f *Font, id uint32, scale, dx float32, shade Shade) bitmap {
	font := f.native
	g := uint16(id)
	var r cgRect
	ct.fontGetBoundingRects(font, ctFontOrientationDefault, &g, &r, 1)
	if r.w <= 0 || r.h <= 0 {
		return bitmap{}
	}
	s, x := float64(scale), float64(dx)
	// Smoothing spreads a glyph up to a pixel further.
	pad := 1
	if f.shaded {
		pad = 2
	}
	// Core Graphics' y goes up: the box from r.y to r.y+r.h above the
	// baseline is from -(r.y+r.h) to -r.y below it.
	left := int(math.Floor(r.x*s+x)) - pad
	right := int(math.Ceil((r.x+r.w)*s+x)) + pad
	top := int(math.Floor(-(r.y+r.h)*s)) - pad
	bottom := int(math.Ceil(-r.y*s)) + pad
	w, h := right-left, bottom-top
	if w > 2048 || h > 2048 {
		return bitmap{}
	}
	color := e.isColor(font)
	var pix []byte
	var ctx uintptr
	if color {
		pix = make([]byte, 4*w*h)
		ctx = ct.bitmapContextCreate(unsafe.Pointer(&pix[0]), w, h, 8, 4*w, e.srgb, cgImageAlphaPremultipliedLst|cgBitmapByteOrder32Big)
	} else {
		pix = make([]byte, w*h)
		ctx = ct.bitmapContextCreate(unsafe.Pointer(&pix[0]), w, h, 8, w, 0, cgImageAlphaOnly)
	}
	if ctx == 0 {
		return bitmap{}
	}
	ct.contextAntialias(ctx, true)
	// Font smoothing, unless the user turned it off (AppleFontSmoothing),
	// emboldens glyphs more the lighter the fill color is, even in a
	// context of alpha alone: the gray of the shade sets how much.
	ct.contextSmoothFonts(ctx, f.shaded)
	ct.contextAllowSubpixelPos(ctx, true)
	ct.contextSubpixelPos(ctx, true)
	ct.contextAllowQuantize(ctx, false)
	ct.contextQuantize(ctx, false)
	fill := 0.0
	if f.shaded {
		fill = shadeGray(shade)
	}
	ct.contextSetFill(ctx, fill, fill, fill, 1)
	ct.contextScaleCTM(ctx, s, s)
	// The bitmap's rows go from its top; the context's origin is at its
	// bottom left, the baseline bottom pixels above it.
	pos := cgPoint{(x - float64(left)) / s, float64(bottom) / s}
	ct.fontDrawGlyphs(font, &g, &pos, 1, ctx)
	ct.contextRelease(ctx)
	runtime.KeepAlive(pix)
	return bitmap{left: left, top: top, w: w, h: h, pix: pix, color: color}
}

// shadeGray returns the sRGB gray of the relative luminance a shade
// stands for.
func shadeGray(shade Shade) float64 {
	y := float64(shade) / Shades
	if y <= 0.0031308 {
		return y * 12.92
	}
	return 1.055*math.Pow(y, 1/2.4) - 0.055
}

func (e *coreText) register(data []byte, family string) error {
	if ct.descriptorsFromData == nil {
		return errors.New("mygo: adding fonts needs macOS 10.13 or later")
	}
	if len(data) == 0 {
		return errors.New("mygo: no font data")
	}
	d := ct.dataCreate(0, &data[0], len(data))
	defer ct.release(d)
	descs := ct.descriptorsFromData(d)
	if descs == 0 {
		return errors.New("mygo: cannot add font: not a font Core Text reads")
	}
	defer ct.release(descs)
	n := ct.arrayGetCount(descs)
	if n == 0 {
		return errors.New("mygo: cannot add font: not a font Core Text reads")
	}
	for i := range n {
		desc := ct.arrayGetValueAtIndex(descs, i)
		face := registeredFace{desc: ct.retain(desc)}
		var name string
		if s := ct.descriptorAttribute(desc, ct.familyNameAttribute); s != 0 {
			name = goString(s)
			ct.release(s)
		}
		if traits := ct.descriptorAttribute(desc, ct.traitsAttribute); traits != 0 {
			if w := ct.dictionaryGetValue(traits, ct.weightTrait); w != 0 {
				ct.numberGetValue(w, cfNumberFloat64Type, unsafe.Pointer(&face.weight))
			}
			if s := ct.dictionaryGetValue(traits, ct.symbolicTrait); s != 0 {
				var v int64
				ct.numberGetValue(s, cfNumberSInt64Type, unsafe.Pointer(&v))
				face.italic = v&ctFontItalicTrait != 0
			}
			ct.release(traits)
		}
		for _, f := range []string{name, family} {
			if f != "" {
				key := strings.ToLower(f)
				e.registered[key] = append(e.registered[key], face)
			}
		}
	}
	for _, f := range e.primary {
		ct.release(f)
	}
	clear(e.primary)
	return nil
}

func (e *coreText) fontCount() int {
	n := 0
	for _, fs := range e.fonts {
		n += len(fs)
	}
	return n
}

// forgetFonts lets go of the CTFonts of the Fonts made so far, and of
// what is known of them; the fonts of styles stay.
func (e *coreText) forgetFonts() {
	for _, fs := range e.fonts {
		for _, f := range fs {
			ct.release(f.native)
		}
	}
	clear(e.fonts)
	clear(e.color)
}
