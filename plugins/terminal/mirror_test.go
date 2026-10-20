package terminal

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal/internal/vt"
)

var debugFrame = os.Getenv("MIRROR_DEBUG")

// mirrored is a terminal and another showing its Mirror's frames.
type mirrored struct {
	t      *testing.T
	src    *Terminal
	dst    *Terminal
	m      *Mirror
	frames int
}

func newMirrored(t *testing.T, cols, rows int, o Options) *mirrored {
	t.Helper()
	o.Conn = newPipe()
	src, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := New(Options{Conn: newPipe()})
	if err != nil {
		t.Fatal(err)
	}
	src.Resize(cols, rows)
	dst.Resize(cols, rows)
	m, err := src.NewMirror()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close(); src.Close(); dst.Close() })
	x := &mirrored{t: t, src: src, dst: dst, m: m}
	x.frame("start")
	return x
}

// feed writes output to the source, then shows the frame and checks it.
func (x *mirrored) feed(label string, out string) {
	x.t.Helper()
	x.src.Feed([]byte(out))
	x.frame(label)
}

func (x *mirrored) resize(cols, rows int) {
	x.t.Helper()
	x.src.Resize(cols, rows)
	x.dst.Resize(cols, rows)
	x.frame(fmt.Sprintf("resize %dx%d", cols, rows))
}

func (x *mirrored) frame(label string) {
	x.t.Helper()
	f, due := x.m.Frame()
	if due {
		return // a synchronized update holds it back: the next frame shows
	}
	if debugFrame != "" && strings.Contains(label, debugFrame) {
		x.src.mu.Lock()
		for y := x.src.term.ScrollbackRows(); y < x.src.term.ScrollbackRows()+3; y++ {
			fmt.Printf("src row %d wrap=%v end=%d vt=%q\n", y, x.src.term.RowWrapped(y), x.src.term.RowEnd(y), x.src.term.RowVT(y))
		}
		x.src.mu.Unlock()
		fmt.Printf("FRAME %q\n", f)
	}
	x.dst.Feed(f)
	x.frames++
	x.same(label)
}

// same checks that the other terminal shows what the source does: every
// row of its screen and scrollback, its wraps, cursor and modes.
func (x *mirrored) same(label string) {
	x.t.Helper()
	a, b := x.src, x.dst
	a.mu.Lock()
	b.mu.Lock()
	defer a.mu.Unlock()
	defer b.mu.Unlock()
	ta, tb := a.term, b.term
	fail := func(format string, args ...any) {
		x.t.Helper()
		x.t.Fatalf("%s (frame %d): %s", label, x.frames, fmt.Sprintf(format, args...))
	}
	if ta.AltScreen() != tb.AltScreen() {
		fail("alt screen %v, mirrored %v", ta.AltScreen(), tb.AltScreen())
	}
	ca, ra := ta.Size()
	if cb, rb := tb.Size(); ca != cb || ra != rb {
		fail("size %dx%d, mirrored %dx%d", ca, ra, cb, rb)
	}
	sa, sb := ta.ScrollbackRows(), tb.ScrollbackRows()
	if ta.AltScreen() {
		sa, sb = 0, 0
	} else if sa != sb {
		fail("scrollback %d rows, mirrored %d", sa, sb)
	}
	for y := 0; y < sa+ra; y++ {
		va, vb := ta.RowVT(y), tb.RowVT(y)
		if ta.RowWrapped(y) {
			// A row that wraps ends where the next starts: the other
			// terminal, wrapping it by printing, fills its empty cells with
			// blanks, which look the same.
			va, vb = trimBlanks(va), trimBlanks(vb)
		}
		if !bytes.Equal(va, vb) {
			fail("row %d (screen row %d) is %q, mirrored %q", y, y-sa, va, vb)
		}
		// The screen's last row wraps into nothing: the other terminal
		// cannot wrap its own without scrolling, and nothing follows to
		// join, until the screen scrolls and the mirror writes it again.
		if wa, wb := ta.RowWrapped(y), tb.RowWrapped(y); wa != wb && y != sa+ra-1 {
			fail("row %d wraps %v, mirrored %v", y, wa, wb)
		}
	}
	xa, ya := ta.Cursor()
	if xb, yb := tb.Cursor(); xa != xb || ya != yb {
		fail("cursor %d,%d, mirrored %d,%d", xa, ya, xb, yb)
	}
	for _, md := range append(mirroredModes, vt.ModeCursorVisible) {
		if ta.Mode(md) != tb.Mode(md) {
			fail("mode %d is %v, mirrored %v", md, ta.Mode(md), tb.Mode(md))
		}
	}
	if ta.KittyKeyboardFlags() != tb.KittyKeyboardFlags() {
		fail("kitty keyboard flags %d, mirrored %d", ta.KittyKeyboardFlags(), tb.KittyKeyboardFlags())
	}
}

func lines(from, to int, prefix string) string {
	var b strings.Builder
	for i := from; i < to; i++ {
		fmt.Fprintf(&b, "%s%d\r\n", prefix, i)
	}
	return b.String()
}

func TestMirrorScrolling(t *testing.T) {
	loadLib(t)
	x := newMirrored(t, 20, 6, Options{})
	x.feed("a few lines", lines(0, 3, "line "))
	x.feed("past the screen", lines(3, 9, "line "))
	x.feed("a screen and more at once", lines(9, 40, "row "))
	x.feed("one more", "last\r\n")
	x.feed("styles", "\x1b[1;31mred\x1b[0m \x1b[48;5;4mbg\x1b[0m \x1b[38;2;10;20;30mrgb\x1b[0m\r\n")
	x.feed("a link", "\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\ after\r\n")
	x.feed("wide characters", "你好世界 😄 ok\r\n")
	x.feed("a wrapped line", strings.Repeat("abcdefghij", 5)+"\r\n")
	x.feed("a wrapped line scrolling off", lines(0, 8, "after "))
	x.feed("a wide character at the edge", strings.Repeat("x", 19)+"世"+"\r\n")
	x.feed("a prompt without a newline", "$ ")
}

func TestMirrorEdits(t *testing.T) {
	loadLib(t)
	x := newMirrored(t, 20, 6, Options{})
	x.feed("fill", lines(0, 10, "l"))
	x.feed("overwrite a row", "\x1b[2;1Hchanged\x1b[K")
	x.feed("insert lines", "\x1b[3;1H\x1b[2L")
	x.feed("delete lines", "\x1b[2;1H\x1b[1M")
	x.feed("a scrolling region", "\x1b[2;4r\x1b[4;1H"+lines(0, 5, "in region "))
	x.feed("the region reset", "\x1b[r\x1b[6;1H")
	x.feed("clear the screen", "\x1b[2J\x1b[H")
	x.feed("after the clear", lines(0, 3, "new "))
	x.feed("clear the scrollback", "\x1b[3J")
	x.feed("after clearing the scrollback", lines(0, 9, "more "))
	x.feed("modes", "\x1b[?1h\x1b[?2004h\x1b[?1000h\x1b[?1006h\x1b[?1004h\x1b[>1u\x1b[?25l")
	x.feed("modes off", "\x1b[?1l\x1b[?2004l\x1b[?1000l\x1b[?1006l\x1b[<u\x1b[?25h")
}

func TestMirrorAltScreen(t *testing.T) {
	loadLib(t)
	x := newMirrored(t, 20, 6, Options{})
	x.feed("history", lines(0, 10, "shell "))
	x.feed("enter the alternate screen", "\x1b[?1049h\x1b[H\x1b[2Jfull screen\x1b[3;3Hthere")
	x.feed("draw on it", "\x1b[5;1H"+strings.Repeat("-", 20))
	x.feed("leave it", "\x1b[?1049l")
	x.feed("after", "$ ls\r\n")
}

func TestMirrorResize(t *testing.T) {
	loadLib(t)
	x := newMirrored(t, 20, 6, Options{})
	x.feed("history", lines(0, 12, "line "))
	x.feed("a long line", strings.Repeat("0123456789", 4)+"\r\n$ ")
	x.resize(30, 8)
	x.feed("after growing", "more\r\n")
	x.resize(12, 5)
	x.feed("after shrinking", lines(0, 3, "s"))
}

func TestMirrorScrollbackLimit(t *testing.T) {
	loadLib(t)
	x := newMirrored(t, 20, 5, Options{Scrollback: 1})
	for i := range 40 {
		x.feed(fmt.Sprintf("burst %d", i), lines(i*100, i*100+60, "x"))
	}
}

// Claude Code's output under a screen of history, fed in uneven pieces, as
// it reaches a server: the frames keep the cursor where it is, on its
// input line, not a row above.
func TestMirrorClaudeCode(t *testing.T) {
	loadLib(t)
	raw, err := os.ReadFile("testdata/claude-code.vt")
	if err != nil {
		t.Skip(err)
	}
	x := newMirrored(t, 120, 36, Options{})
	x.feed("history", lines(1, 101, ""))
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; len(raw) > 0; i++ {
		n := min(len(raw), 1+r.IntN(400))
		x.feed(fmt.Sprintf("piece %d", i), string(raw[:n]))
		raw = raw[n:]
	}
}

// Thousands of random edits, of every kind the mirror follows, each frame
// checked.
func TestMirrorRandom(t *testing.T) {
	loadLib(t)
	for seed := uint64(1); seed <= 3; seed++ {
		r := rand.New(rand.NewPCG(seed, 7))
		x := newMirrored(t, 16, 6, Options{Scrollback: 2})
		words := []string{"a", "hello", "世界", "😄", "\x1b[1;32mgreen\x1b[0m", "\x1b[48;5;3m  \x1b[0m", "\x1b]8;;https://x.y\x1b\\ln\x1b]8;;\x1b\\", strings.Repeat("w", 21)}
		ops := []func() string{
			func() string { return words[r.IntN(len(words))] },
			func() string { return "\r\n" },
			func() string { return lines(0, 1+r.IntN(20), "r") },
			func() string { return fmt.Sprintf("\x1b[%d;%dH", 1+r.IntN(6), 1+r.IntN(16)) },
			func() string { return []string{"\x1b[K", "\x1b[1K", "\x1b[2K", "\x1b[J", "\x1b[1J"}[r.IntN(5)] },
			func() string { return fmt.Sprintf("\x1b[%dL", 1+r.IntN(3)) },
			func() string { return fmt.Sprintf("\x1b[%dM", 1+r.IntN(3)) },
			func() string { return fmt.Sprintf("\x1b[%dS", 1+r.IntN(3)) },
			func() string { return fmt.Sprintf("\x1b[%dT", 1+r.IntN(3)) },
			func() string { return []string{"\x1b[2;5r", "\x1b[r"}[r.IntN(2)] },
			func() string { return []string{"\x1b[?1049h", "\x1b[?1049l"}[r.IntN(2)] },
			func() string {
				return []string{"\x1b[?25l", "\x1b[?25h", "\x1b[?2004h", "\x1b[?1000h\x1b[?1006h", "\x1b[?1000l"}[r.IntN(5)]
			},
			func() string { return "\x1bM" }, // reverse index
		}
		for i := range 1000 {
			var b strings.Builder
			for range 1 + r.IntN(4) {
				b.WriteString(ops[r.IntN(len(ops))]())
			}
			x.feed(fmt.Sprintf("seed %d op %d %q", seed, i, b.String()), b.String())
		}
	}
}

// trimBlanks drops the blanks at the end of a row's VT sequences, before
// its last style reset.
func trimBlanks(b []byte) []byte {
	b = bytes.TrimSuffix(b, []byte("\x1b[0m"))
	return bytes.TrimRight(b, " ")
}

// History is what shows, down to the cursor, with the cursor under it: a
// program's interface below the cursor, as an agent's status bar, goes.
func TestHistory(t *testing.T) {
	loadLib(t)
	src, _ := New(Options{Conn: newPipe()})
	defer src.Close()
	src.Resize(20, 6)
	src.Feed([]byte(lines(0, 10, "line ") + strings.Repeat("abcdefghij", 3) + "\r\n\x1b[1;31mred\x1b[0m\r\n> input\r\nstatus bar\x1b[1A\x1b[3G"))
	h := src.History()
	dst, _ := New(Options{Conn: newPipe()})
	defer dst.Close()
	dst.Resize(20, 6)
	dst.Feed(h)
	text := dst.Text()
	for _, want := range []string{"line 0", "line 9", strings.Repeat("abcdefghij", 3), "red", "> input"} {
		if !strings.Contains(text, want) {
			t.Errorf("history lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "status bar") {
		t.Errorf("history kept what was below the cursor:\n%s", text)
	}
	dst.mu.Lock()
	x, y := dst.term.Cursor()
	row, _ := dst.term.RowText(y)
	dst.mu.Unlock()
	if x != 0 || strings.TrimSpace(string(row)) != "" {
		t.Errorf("the cursor is at %d on %q, not at the start of a row of its own", x, string(row))
	}
	// Soft wraps stay soft: the long line is one line.
	if !strings.Contains(text, strings.Repeat("abcdefghij", 3)+"\n") {
		t.Errorf("the wrapped line did not stay one line:\n%s", text)
	}
	alt, _ := New(Options{Conn: newPipe()})
	defer alt.Close()
	alt.Feed([]byte("\x1b[?1049hfull screen"))
	if h := alt.History(); len(h) != 0 {
		t.Errorf("history of a full-screen program: %q", h)
	}
}
