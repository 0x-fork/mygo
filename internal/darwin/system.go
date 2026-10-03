//go:build darwin

package darwin

import (
	"errors"
	"fmt"
	"math"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

// Clipboard (NSPasteboard).

type clipboard struct{}

const (
	utString = "public.utf8-plain-text"
	utHTML   = "public.html"
	utPNG    = "public.png"
	utTIFF   = "public.tiff"
)

func pasteboard() id { return send(class("NSPasteboard"), "generalPasteboard") }

func (clipboard) ReadText() string {
	var s string
	withPool(func() { s = goString(send(pasteboard(), "stringForType:", uintptr(nsString(utString)))) })
	return s
}

func (clipboard) WriteText(text string) {
	withPool(func() {
		pb := pasteboard()
		send(pb, "clearContents")
		send(pb, "setString:forType:", uintptr(nsString(text)), uintptr(nsString(utString)))
	})
}

func (clipboard) ReadHTML() string {
	var s string
	withPool(func() { s = goString(send(pasteboard(), "stringForType:", uintptr(nsString(utHTML)))) })
	return s
}

func (clipboard) WriteHTML(markup string) {
	withPool(func() {
		pb := pasteboard()
		send(pb, "clearContents")
		send(pb, "setString:forType:", uintptr(nsString(markup)), uintptr(nsString(utHTML)))
	})
}

func (clipboard) ReadImage() []byte {
	var out []byte
	withPool(func() {
		pb := pasteboard()
		if data := send(pb, "dataForType:", uintptr(nsString(utPNG))); data != 0 {
			out = goBytes(data)
			return
		}
		if data := send(pb, "dataForType:", uintptr(nsString(utTIFF))); data != 0 {
			rep := send(class("NSBitmapImageRep"), "imageRepWithData:", uintptr(data))
			if rep != 0 {
				out = goBytes(send(rep, "representationUsingType:properties:", 4, uintptr(send(class("NSDictionary"), "dictionary"))))
			}
		}
	})
	return out
}

func (clipboard) WriteImage(png []byte) error {
	var err error
	withPool(func() {
		img := autorelease(send(send(class("NSImage"), "alloc"), "initWithData:", uintptr(nsData(png))))
		if img == 0 {
			err = errInvalidImage
			return
		}
		pb := pasteboard()
		send(pb, "clearContents")
		send(pb, "setData:forType:", uintptr(nsData(png)), uintptr(nsString(utPNG)))
	})
	return err
}

func (clipboard) Clear() { send(pasteboard(), "clearContents") }

func (clipboard) AvailableFormats() []string {
	var out []string
	withPool(func() {
		for _, t := range arrayItems(send(pasteboard(), "types")) {
			out = append(out, goString(t))
		}
	})
	return out
}

// Shell (NSWorkspace).

type shell struct{}

func workspace() id { return send(class("NSWorkspace"), "sharedWorkspace") }

func (shell) OpenExternal(url string) error {
	var err error
	withPool(func() {
		u := nsURL(url)
		if u == 0 {
			err = fmt.Errorf("mygo: invalid URL %q", url)
			return
		}
		if !sendBool(workspace(), "openURL:", uintptr(u)) {
			err = fmt.Errorf("mygo: no application can open %q", url)
		}
	})
	return err
}

func (shell) OpenPath(path string) error {
	var err error
	withPool(func() {
		if !sendBool(workspace(), "openURL:", uintptr(fileURL(path))) {
			err = fmt.Errorf("mygo: cannot open %q", path)
		}
	})
	return err
}

func (shell) ShowItemInFolder(path string) {
	withPool(func() {
		send(workspace(), "activateFileViewerSelectingURLs:", uintptr(nsArray(fileURL(path))))
	})
}

func (shell) TrashItem(path string) error {
	var err error
	withPool(func() {
		var nserr id
		fm := send(class("NSFileManager"), "defaultManager")
		if !sendBool(fm, "trashItemAtURL:resultingItemURL:error:", uintptr(fileURL(path)), 0, uintptr(unsafe.Pointer(&nserr))) {
			err = nsError(nserr)
			if err == nil {
				err = errors.New("mygo: cannot move to trash")
			}
		}
	})
	return err
}

func (shell) Beep() { nsBeep() }

// Screen (NSScreen).

type screen struct{}

func (screen) Displays() []platform.Display {
	var out []platform.Display
	withPool(func() {
		screens := arrayItems(send(class("NSScreen"), "screens"))
		if len(screens) == 0 {
			return
		}
		key := nsString("NSScreenNumber")
		for i, s := range screens {
			num := send(send(s, "deviceDescription"), "objectForKey:", uintptr(key))
			displayID := uint32(send(num, "unsignedIntValue"))
			d := platform.Display{
				ID:          int64(displayID),
				Bounds:      rectFromMac(msgRect(s, sel("frame"))),
				WorkArea:    rectFromMac(msgRect(s, sel("visibleFrame"))),
				ScaleFactor: msgFloat(s, sel("backingScaleFactor")),
				Rotation:    int(math.Round(cgDisplayRotation(displayID))),
				Internal:    cgDisplayIsBuiltin(displayID),
				Primary:     i == 0,
			}
			if respondsTo(s, "localizedName") {
				d.Label = goString(send(s, "localizedName"))
			}
			out = append(out, d)
		}
	})
	return out
}

func (screen) CursorPoint() platform.Point {
	p := msgPoint(class("NSEvent"), sel("mouseLocation"))
	return platform.Point{X: int(math.Round(p.X)), Y: int(math.Round(primaryScreenHeight() - p.Y))}
}

// Theme (NSAppearance).

type theme struct{ b *Backend }

// UIFont is Core Text's to find: the system font.
func (theme) UIFont() string { return "" }

func (t theme) IsDark() bool {
	var dark bool
	withPool(func() {
		appearance := send(t.b.app, "effectiveAppearance")
		names := nsArray(nsString("NSAppearanceNameAqua"), nsString("NSAppearanceNameDarkAqua"))
		dark = goString(send(appearance, "bestMatchFromAppearancesWithNames:", uintptr(names))) == "NSAppearanceNameDarkAqua"
	})
	return dark
}

func (t theme) SetSource(source string) {
	withPool(func() {
		var appearance id
		switch source {
		case "light":
			appearance = send(class("NSAppearance"), "appearanceNamed:", uintptr(nsString("NSAppearanceNameAqua")))
		case "dark":
			appearance = send(class("NSAppearance"), "appearanceNamed:", uintptr(nsString("NSAppearanceNameDarkAqua")))
		}
		send(t.b.app, "setAppearance:", uintptr(appearance))
	})
}
