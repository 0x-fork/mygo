package vt

import (
	"fmt"
	"strconv"
	"strings"
	"unsafe"
)

// The rows of a terminal, its screen's and its scrollback's, as VT
// sequences for another terminal to show: addressed from the top of the
// scrollback (GHOSTTY_POINT_TAG_SCREEN).

const (
	pointScreen = 2

	rowDataWrap      = 1
	rowDataHyperlink = 5

	dataCursorPendingWrap  = 5
	dataKittyKeyboardFlags = 8
	dataScrollbackRows     = 15
	dataModifyOtherKeys    = 41
)

// ScrollbackRows returns how many rows the scrollback holds.
func (t *Terminal) ScrollbackRows() int {
	var n uintptr
	t.get(dataScrollbackRows, unsafe.Pointer(&n))
	return int(n)
}

// KittyKeyboardFlags returns the flags of the kitty keyboard protocol
// programs set.
func (t *Terminal) KittyKeyboardFlags() int {
	var f uint8
	t.get(dataKittyKeyboardFlags, unsafe.Pointer(&f))
	return int(f)
}

// ModifyOtherKeys reports whether xterm's modifyOtherKeys mode 2 is on.
func (t *Terminal) ModifyOtherKeys() bool {
	var v bool
	return t.get(dataModifyOtherKeys, unsafe.Pointer(&v)) == nil && v
}

// screenRef returns the cell at column x of row y, counted from the top of
// the scrollback.
func (t *Terminal) screenRef(x, y int) (GridRef, bool) {
	ref := GridRef{size: unsafe.Sizeof(GridRef{})}
	p := point{tag: pointScreen, x: uint16(max(0, min(x, 0xFFFF))), y: uint32(max(y, 0))}
	return ref, terminalGridRef(t.h, p, &ref) == 0
}

// rowFlag reports a flag of row y, counted from the top of the scrollback.
func (t *Terminal) rowFlag(y, data int) bool {
	ref, found := t.screenRef(0, y)
	if !found {
		return false
	}
	var row uint64
	if !ok(call(fnGridRefRow, uintptr(unsafe.Pointer(&ref)), uintptr(unsafe.Pointer(&row)))) {
		return false
	}
	var v bool
	return ok(call(fnRowData, uintptr(row), uintptr(data), uintptr(unsafe.Pointer(&v)))) && v
}

// RowWrapped reports whether row y, counted from the top of the
// scrollback, soft-wraps into the next.
func (t *Terminal) RowWrapped(y int) bool { return t.rowFlag(y, rowDataWrap) }

// RowEnd returns how many columns of row y, counted from the top of the
// scrollback, hold something: text, wide characters' second halves
// included, or a style, as a colored background.
func (t *Terminal) RowEnd(y int) int {
	c, _ := t.Size()
	for x := c - 1; x >= 0; x-- {
		raw, found := t.screenCell(x, y)
		if !found {
			continue
		}
		// The second half of a wide character is the character's.
		if cell.tag.of(raw) != 0 || cell.codepoint.of(raw) != 0 || cell.styleID.of(raw) != 0 || Wide(cell.wide.of(raw)) == SpacerTail {
			return x + 1
		}
	}
	return 0
}

// RowSpacerHead reports whether row y, counted from the top of the
// scrollback, ends in a spacer: a wide character that did not fit went on
// to the next row, which it soft-wraps into.
func (t *Terminal) RowSpacerHead(y int) bool {
	c, _ := t.Size()
	raw, found := t.screenCell(c-1, y)
	return found && Wide(cell.wide.of(raw)) == SpacerHead
}

// RowStartsWide reports whether row y, counted from the top of the
// scrollback, starts with a wide character.
func (t *Terminal) RowStartsWide(y int) bool {
	raw, found := t.screenCell(0, y)
	return found && Wide(cell.wide.of(raw)) == WideChar
}

func (t *Terminal) screenCell(x, y int) (uint64, bool) {
	ref, found := t.screenRef(x, y)
	if !found {
		return 0, false
	}
	var raw uint64
	return raw, ok(call(fnGridRefCell, uintptr(unsafe.Pointer(&ref)), uintptr(unsafe.Pointer(&raw))))
}

// RowVT returns row y, counted from the top of the scrollback, as VT
// sequences that print it from its first column: its text, with its styles
// and hyperlinks, without the blank cells at its end.
func (t *Terminal) RowVT(y int) []byte {
	if t.rowFlag(y, rowDataHyperlink) {
		return t.rowCellsVT(y)
	}
	c, _ := t.Size()
	start, ok1 := t.screenRef(0, y)
	end, ok2 := t.screenRef(c-1, y)
	if !ok1 || !ok2 {
		return nil
	}
	sel := Selection{size: unsafe.Sizeof(Selection{}), start: start, end: end}
	opts := formatterOptions{size: unsafe.Sizeof(formatterOptions{}), emit: formatVT, trim: true, selection: uintptr(unsafe.Pointer(&sel))}
	opts.extra.size = unsafe.Sizeof(opts.extra)
	opts.extra.screen.size = unsafe.Sizeof(opts.extra.screen)
	var f uintptr
	if formatterTerminalNew(0, &f, t.h, opts) != 0 {
		return nil
	}
	defer call(fnFormatterFree, f)
	var p, n uintptr
	if !ok(call(fnFormatterFormatAlloc, f, 0, uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&n)))) {
		return nil
	}
	b := []byte(goString(p, n))
	if p != 0 {
		call(fnFree, 0, p, n)
	}
	return b
}

// rowCellsVT is RowVT cell by cell, for rows with hyperlinks, which the
// library's formatter leaves out.
func (t *Terminal) rowCellsVT(y int) []byte {
	var b strings.Builder
	end := t.RowEnd(y)
	link, sgr := "", ""
	var buf [16]uint32
	for x := 0; x < end; x++ {
		ref, found := t.screenRef(x, y)
		if !found {
			break
		}
		var raw uint64
		call(fnGridRefCell, uintptr(unsafe.Pointer(&ref)), uintptr(unsafe.Pointer(&raw)))
		if w := Wide(cell.wide.of(raw)); w == SpacerTail || w == SpacerHead {
			continue
		}
		s := style{size: unsafe.Sizeof(style{})}
		call(fnGridRefStyle, uintptr(unsafe.Pointer(&ref)), uintptr(unsafe.Pointer(&s)))
		if g := sgrOf(s); g != sgr {
			b.WriteString(g)
			sgr = g
		}
		l := ""
		if cell.hyperlink.of(raw) != 0 {
			l = t.Hyperlink(ref)
		}
		if l != link {
			fmt.Fprintf(&b, "\x1b]8;;%s\x1b\\", l)
			link = l
		}
		var n uintptr
		if ok(call(fnGridRefGraphemes, uintptr(unsafe.Pointer(&ref)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n)))) && n > 0 {
			for _, r := range buf[:n] {
				b.WriteRune(rune(r))
			}
		} else {
			b.WriteByte(' ')
		}
	}
	if link != "" {
		b.WriteString("\x1b]8;;\x1b\\")
	}
	b.WriteString("\x1b[0m")
	return []byte(b.String())
}

// sgrOf returns the SGR sequence that sets a style from none.
func sgrOf(s style) string {
	p := []string{"0"}
	add := func(on bool, code string) {
		if on {
			p = append(p, code)
		}
	}
	add(s.bold, "1")
	add(s.faint, "2")
	add(s.italic, "3")
	switch s.underline {
	case 0:
	case 1:
		p = append(p, "4")
	default:
		p = append(p, "4:"+strconv.Itoa(int(s.underline)))
	}
	add(s.blink, "5")
	add(s.inverse, "7")
	add(s.invisible, "8")
	add(s.strikethrough, "9")
	add(s.overline, "53")
	color := func(c styleColor, base string) {
		switch c.tag {
		case colorPalette:
			p = append(p, base+":5:"+strconv.Itoa(int(c.value[0])))
		case colorRGB:
			p = append(p, fmt.Sprintf("%s:2::%d:%d:%d", base, c.value[0], c.value[1], c.value[2]))
		}
	}
	color(s.fg, "38")
	color(s.bg, "48")
	color(s.underlineColor, "58")
	return "\x1b[" + strings.Join(p, ";") + "m"
}
