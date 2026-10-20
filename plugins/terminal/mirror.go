package terminal

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/egoist/mygo/plugins/terminal/internal/vt"
)

// Mirror frames what a terminal shows for another terminal to show, as a
// server that keeps a session's terminal sends its clients: Frame returns
// the VT sequences that bring a terminal showing the frames before to what
// this one shows now. The other terminal, as one whose Conn reads the
// frames, then shows this one's screen and scrollback cell for cell, with
// its cursor and the modes that change what it sends (the cursor keys',
// the keyboard protocols', the mouse's, bracketed paste), and never lays
// out a program's output itself: it cannot come to show something else,
// as a terminal fed the program's output from a snapshot can.
//
// Frames draw rows where they are, and rows that scroll off pass through
// the other terminal's screen in order, so that its scrollback is this
// one's. Soft wraps stay soft. The first frame, and a frame after the
// screen took another size, start the other terminal over. A frame does
// not come while a program holds a synchronized update (mode 2026), for up
// to a second, as the view's frames do not: Frame then reports that one
// is due.
//
// Programs' writes to the clipboard (OSC 52) go on to the other terminal,
// and so do their reads with ClipboardForward, for it to answer.
type Mirror struct {
	t  *Terminal
	rs *vt.RenderState
	// started tells that a frame went; cols and rows are the size it
	// drew, alt that it showed the alternate screen.
	started    bool
	cols, rows int
	alt        bool
	// ids are the identities of the rows the last frame showed, which
	// rows keep as they scroll; sent is how each was drawn, and main the
	// main screen's while the alternate screen shows.
	ids        []vt.RowID
	sent, main [][]byte
	// scrollback is how many rows the main screen's scrollback held at the
	// last frame that showed the main screen.
	scrollback int
	// modes, cursor and title are what the last frame set of them.
	modes, cursor, title string
}

// NewMirror returns a Mirror of the terminal, whose first frame starts the
// other terminal over. Close frees it.
func (t *Terminal) NewMirror() (*Mirror, error) {
	rs, err := vt.NewRenderState()
	if err != nil {
		return nil, err
	}
	return &Mirror{t: t, rs: rs}, nil
}

// Close frees the mirror.
func (m *Mirror) Close() {
	m.t.mu.Lock()
	defer m.t.mu.Unlock()
	if m.rs != nil {
		m.rs.Free()
		m.rs = nil
	}
}

// Reset has the next frame start the other terminal over, as when another
// terminal shows the frames from now.
func (m *Mirror) Reset() {
	m.t.mu.Lock()
	m.started = false
	m.t.mu.Unlock()
}

// Frame returns what brings the other terminal to what this one shows,
// nil when it shows it already; due reports that a frame is due later,
// as one a synchronized update holds back.
func (m *Mirror) Frame() (frame []byte, due bool) {
	t := m.t
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.term == nil || m.rs == nil {
		return nil, false
	}
	if t.term.Mode(vt.ModeSyncOutput) && t.held && time.Since(t.heldSince) < time.Second {
		return nil, true
	}
	var out bytes.Buffer
	m.frame(&out)
	return out.Bytes(), false
}

func (m *Mirror) frame(out *bytes.Buffer) {
	t := m.t
	term := t.term
	cols, rows := term.Size()
	alt := term.AltScreen()
	scrollback := term.ScrollbackRows()
	if !term.AtBottom() {
		term.ScrollToBottom()
	}
	m.rs.Update(term)
	ids := make([]vt.RowID, 0, rows)
	dirty := make([]bool, 0, rows)
	m.rs.Rows()
	for m.rs.NextRow() {
		ids = append(ids, m.rs.RowID())
		dirty = append(dirty, m.rs.RowDirty())
	}
	m.rs.Clean()
	top := scrollback // the screen's first row, from the top of the scrollback
	if alt {
		top = 0
	}
	switch {
	case !m.started || cols != m.cols || rows != m.rows:
		m.restart(out, cols, rows, alt, top)
	case alt != m.alt && !alt && scrollback != m.scrollback:
		// Rows went to the main screen's scrollback before its program
		// took the alternate screen, which hid them: start over.
		m.restart(out, cols, rows, alt, top)
	case alt != m.alt:
		if alt {
			out.WriteString("\x1b[?1049h")
			m.main, m.sent = m.sent, make([][]byte, rows)
		} else {
			out.WriteString("\x1b[?1049l")
			m.sent, m.main = m.main, nil
		}
		m.redraw(out, top, func(int) bool { return true })
	default:
		scrolled := 0
		if !alt {
			scrolled = m.scrolled(ids, scrollback)
		}
		switch {
		case scrolled < 0: // lost track: start over
			m.restart(out, cols, rows, alt, top)
		case scrolled > 0:
			// Rows went to the scrollback: they pass through the other
			// screen's top in order, the screen's rows after them.
			if scrollback < m.scrollback {
				out.WriteString("\x1b[3J")
			}
			m.write(out, top-scrolled, top+rows)
		default:
			if scrollback < m.scrollback {
				out.WriteString("\x1b[3J") // the scrollback was cleared
			}
			m.redraw(out, top, func(y int) bool { return y < len(dirty) && dirty[y] })
		}
	}
	m.started, m.cols, m.rows, m.alt, m.ids = true, cols, rows, alt, ids
	if !alt {
		m.scrollback = scrollback
	}
	m.state(out)
}

// scrolled returns how many rows scrolled off the screen since the last
// frame, which ids are the rows of now, or -1 when it cannot tell.
func (m *Mirror) scrolled(ids []vt.RowID, scrollback int) int {
	if len(ids) == 0 || len(m.ids) == 0 {
		return -1
	}
	k := -1
	for i, id := range m.ids {
		if id == ids[0] {
			k = i
			break
		}
	}
	grew := scrollback - m.scrollback
	switch {
	case k == 0 && grew > 0:
		// The screen's rows kept their places, the scrollback grew under
		// a full screen's worth of rows: more than a screen scrolled.
		return grew
	case k >= 0:
		return k
	case grew > 0:
		return grew
	}
	return -1
}

// restart starts the other terminal over: its scrollback and screen, then
// this one's, row by row.
func (m *Mirror) restart(out *bytes.Buffer, cols, rows int, alt bool, top int) {
	out.WriteString("\x1b[?1049l\x1b[0m\x1b[H\x1b[2J\x1b[3J")
	m.modes, m.cursor = "", ""
	m.sent = make([][]byte, rows)
	m.main = nil
	if alt {
		// The main screen of a full-screen program's terminal is not to be
		// read: the other one's shows from when the program ends.
		out.WriteString("\x1b[?1049h")
		m.redraw(out, 0, func(int) bool { return true })
		return
	}
	m.write(out, 0, top+rows)
}

// write draws rows from up to to, counted from the top of the scrollback,
// one after the other from the other screen's top, so that those before
// the screen's go to its scrollback, as they went to this one's.
func (m *Mirror) write(out *bytes.Buffer, from, to int) {
	term := m.t.term
	cols, rows := term.Size()
	screen := to - rows
	out.WriteString("\x1b[H")
	for y := from; y < to; y++ {
		last := y == to-1
		b := m.row(out, y, last, y > from && term.RowWrapped(y-1), cols)
		if y >= screen {
			m.sent[y-screen] = b
		}
		if !last && !term.RowWrapped(y) {
			out.WriteString("\r\n")
		}
	}
}

// redraw draws again the rows of the screen, from top, that changed, and
// those of the soft-wrapped lines they are in, from their lines' first rows.
func (m *Mirror) redraw(out *bytes.Buffer, top int, changed func(y int) bool) {
	term := m.t.term
	cols, rows := term.Size()
	if len(m.sent) != rows {
		m.sent = make([][]byte, rows)
	}
	encoded := make([][]byte, rows)
	for y := range rows {
		if changed(y) || m.sent[y] == nil {
			encoded[y] = rowKey(term, top+y)
		}
	}
	for y := 0; y < rows; y++ {
		if encoded[y] == nil || bytes.Equal(encoded[y], m.sent[y]) {
			continue
		}
		// From the first row of its line, which a soft wrap continues.
		s := y
		for s > 0 && term.RowWrapped(top+s-1) {
			s--
		}
		fmt.Fprintf(out, "\x1b[%dH", s+1)
		for first := s; s < rows; s++ {
			last := s == rows-1 || !term.RowWrapped(top+s)
			m.sent[s] = m.row(out, top+s, last, s > first, cols)
			if last {
				break
			}
		}
		y = s
	}
}

// row draws row y, counted from the top of the scrollback, from the
// cursor's column 0: in full, clearing what was after it, or, soft wrapped
// into the next, out to its last column for the other terminal to wrap it
// too. A row is cleared before it is drawn, but for one a soft wrap
// continues, cont,
// starts with the cursor still at the end of the row before, waiting to
// wrap, which a clear would clear instead; with nothing on it, a blank
// wraps there first, and the clear after takes it. It returns what identifies how it drew it.
func (m *Mirror) row(out *bytes.Buffer, y int, last, cont bool, cols int) []byte {
	term := m.t.term
	key := rowKey(term, y)
	out.WriteString("\x1b[0m")
	switch {
	case !cont:
		// Cleared whole, which also undoes a wrap it had.
		out.WriteString("\x1b[2K")
	case len(key) == 1:
		// The blank wraps the row before into this one, then goes.
		out.WriteString(" \r")
	}
	out.Write(key[1:])
	out.WriteString("\x1b[0m")
	if key[0] == 1 && !last {
		// Out to the last column, which the next character wraps from;
		// short of it when a wide character did not fit there, for the
		// other terminal to wrap it too.
		end, to := term.RowEnd(y), cols
		if term.RowSpacerHead(y) && term.RowStartsWide(y+1) {
			to--
		}
		if end < to {
			out.Write(bytes.Repeat([]byte{' '}, to-end))
		}
	} else if term.RowEnd(y) < cols {
		out.WriteString("\x1b[K")
	}
	return key
}

// rowKey returns row y as VT sequences, after a byte telling whether it
// soft-wraps: what tells whether the other terminal shows it as it is.
func rowKey(term *vt.Terminal, y int) []byte {
	wrap := byte(0)
	if term.RowWrapped(y) {
		wrap = 1
	}
	return append([]byte{wrap}, term.RowVT(y)...)
}

// mirroredModes are the modes the other terminal takes: those that change
// what it sends, as the keys and the mouse, and how it draws.
var mirroredModes = []vt.Mode{
	vt.ModeCursorKeys, 66, // DECCKM, DECNKM (the keypad's)
	vt.ModeFocusEvent, vt.ModeAltScroll, vt.ModeBracketedPaste, vt.ModeGraphemes,
	9, 1000, 1002, 1003, 1005, 1006, 1015, 1016, // the mouse's
	5, // DECSCNM, the screen in reverse video
}

// state sets the cursor, the modes and the title, where they changed, and
// passes on programs' uses of the clipboard.
func (m *Mirror) state(out *bytes.Buffer) {
	t := m.t
	term := t.term
	var modes bytes.Buffer
	for _, md := range mirroredModes {
		v := byte('l')
		if term.Mode(md) {
			v = 'h'
		}
		fmt.Fprintf(&modes, "\x1b[?%d%c", md, v)
	}
	mok := 1
	if term.ModifyOtherKeys() {
		mok = 2
	}
	fmt.Fprintf(&modes, "\x1b[=%d;1u\x1b[>4;%dm", term.KittyKeyboardFlags(), mok)
	if shape := term.MouseShape(); shape >= 0 && shape < len(shapeNames) {
		fmt.Fprintf(&modes, "\x1b]22;%s\x1b\\", shapeNames[shape])
	}
	if s := modes.String(); s != m.modes {
		out.WriteString(s)
		m.modes = s
	}
	if title := term.Title(); title != m.title {
		fmt.Fprintf(out, "\x1b]2;%s\x1b\\", title)
		m.title = title
	}
	for _, text := range t.clipOut {
		fmt.Fprintf(out, "\x1b]52;c;%s\x1b\\", base64.StdEncoding.EncodeToString([]byte(text)))
	}
	t.clipOut = nil
	for _, primary := range t.clipForward {
		sel := "c"
		if primary {
			sel = "p"
		}
		fmt.Fprintf(out, "\x1b]52;%s;?\x1b\\", sel)
	}
	t.clipForward = nil
	c := m.rs.Cursor()
	x, y := term.Cursor()
	var cur bytes.Buffer
	shape := map[vt.CursorStyle]int{vt.CursorBlock: 2, vt.CursorUnderline: 4, vt.CursorBar: 6}[c.Style]
	if shape == 0 {
		shape = 2
	}
	if c.Blinking {
		shape--
	}
	visible := "l"
	if c.Visible {
		visible = "h"
	}
	fmt.Fprintf(&cur, "\x1b[0m\x1b[%d;%dH\x1b[?25%s\x1b[%d q", y+1, x+1, visible, shape)
	// After rows, the cursor goes back, though it did not move.
	if s := cur.String(); s != m.cursor || out.Len() > 0 {
		out.WriteString(s)
		m.cursor = s
	}
}

// shapeNames are the pointer shapes of OSC 22, in Ghostty's order.
var shapeNames = []string{"default", "context-menu", "help", "pointer", "progress", "wait", "cell", "crosshair",
	"text", "vertical-text", "alias", "copy", "move", "no-drop", "not-allowed", "grab", "grabbing", "all-scroll",
	"col-resize", "row-resize", "n-resize", "e-resize", "s-resize", "w-resize", "ne-resize", "nw-resize",
	"se-resize", "sw-resize", "ew-resize", "ns-resize", "nesw-resize", "nwse-resize", "zoom-in", "zoom-out"}

// History returns what the terminal shows as rows of VT sequences, for a
// terminal that takes the session over, as after the program that wrote
// them ended: the scrollback and the screen down to the cursor's row, or
// to the last row with something on it when that is above, without the
// cursor and the modes. Fed to a terminal of the same width, it leaves the
// cursor below what it shows, for the next program to start under it, not
// over it. A full-screen program's screen has no history: History returns
// nothing while the alternate screen shows.
func (t *Terminal) History() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	term := t.term
	if term == nil || term.AltScreen() {
		return nil
	}
	_, y := term.Cursor()
	end := term.ScrollbackRows() + y // the last row, from the top of the scrollback
	for end >= 0 && term.RowEnd(end) == 0 {
		end--
	}
	var out bytes.Buffer
	for y := 0; y <= end; y++ {
		key := rowKey(term, y)
		out.Write(key[1:])
		out.WriteString("\x1b[0m")
		if key[0] == 1 && y < end {
			// Soft wrapped: on to the last column, for the next row to wrap.
			cols, _ := term.Size()
			if e, to := term.RowEnd(y), cols; e < to {
				if term.RowSpacerHead(y) && term.RowStartsWide(y+1) {
					to--
				}
				out.Write(bytes.Repeat([]byte{' '}, max(0, to-e)))
			}
			continue
		}
		out.WriteString("\r\n")
	}
	return out.Bytes()
}
