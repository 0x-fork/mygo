package ui

import (
	"fmt"
	"image"
	"strings"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/raster"
	"github.com/egoist/mygo/internal/scene"
)

// headless renders frames in memory with the software renderer.
type headless struct {
	w, h, scale float32
	img         raster.Renderer
	requested   bool
	dark        bool
	clipboard   string
	cursor      Cursor
	ime         platform.TextInputState
	opened      []string
	bar         TitleBar
	access      *platform.AccessTree
	// menu is the context menu shown, and chosen takes its choice.
	menu   *platform.Menu
	chosen func(id int)
}

func (h *headless) size() (float32, float32, float32) { return h.w, h.h, h.scale }
func (h *headless) present(s *scene.Scene) {
	h.img.Render(s)
}
func (h *headless) requestFrame()                              { h.requested = true }
func (h *headless) setCursor(c Cursor)                         { h.cursor = c }
func (h *headless) setTextInput(t platform.TextInputState)     { h.ime = t }
func (h *headless) updateAccessibility(t *platform.AccessTree) { h.access = t }
func (h *headless) readClipboard() string                      { return h.clipboard }
func (h *headless) writeClipboard(s string)                    { h.clipboard = s }
func (h *headless) startDrag()                                 {}
func (h *headless) titleBarDoubleClicked()                     {}
func (h *headless) isDark() bool                               { return h.dark }
func (h *headless) titleBar() TitleBar                         { return h.bar }
func (h *headless) invalidate()                                { h.requested = true }
func (h *headless) openURL(u string)                           { h.opened = append(h.opened, u) }
func (h *headless) popupMenu(m *platform.Menu, x, y float32, chosen func(int)) {
	h.menu, h.chosen = m, chosen
}
func (h *headless) image() *image.RGBA {
	m := &h.img.Image
	return &image.RGBA{Pix: m.RGBA(), Stride: 4 * m.W, Rect: image.Rect(0, 0, m.W, m.H)}
}

// Render draws a frame of view in a window of width×height DIPs at scale
// device pixels per DIP, without a window: for snapshots and tests.
func Render(view func(c *Context), width, height int, scale float32) *image.RGBA {
	t := NewTester(view, width, height)
	t.SetScale(scale)
	return t.Image()
}

// Tester runs a view without a window, as a test drives it: it renders
// frames in memory and sends the view pointer and keyboard input, between
// which it settles the frames the view asks for.
type Tester struct {
	rt *engine
	h  *headless
}

// NewTester starts testing view in a window of width×height DIPs.
func NewTester(view func(c *Context), width, height int) *Tester {
	h := &headless{w: float32(width), h: float32(height), scale: 1}
	t := &Tester{rt: newRuntime(view, h), h: h}
	t.rt.collect = true
	t.settle()
	return t
}

// settle runs frames until the view asks for no more, or 20 of them.
func (t *Tester) settle() {
	for i := 0; i < 20; i++ {
		t.h.requested = false
		t.rt.runFrame()
		if !t.h.requested {
			return
		}
	}
}

func (t *Tester) send(ev platform.SurfaceEvent) {
	t.rt.event(ev)
	t.settle()
}

// SetSize resizes the window.
func (t *Tester) SetSize(width, height int) {
	t.h.w, t.h.h = float32(width), float32(height)
	t.settle()
}

// SetScale sets the device pixels per DIP.
func (t *Tester) SetScale(scale float32) {
	t.h.scale = scale
	t.settle()
}

// SetTitleBar sets the room the window controls of a window with a hidden
// title bar take, which Context.TitleBar returns.
func (t *Tester) SetTitleBar(bar TitleBar) {
	t.h.bar = bar
	t.Frame()
}

// SetDark switches between the light and the dark appearance.
func (t *Tester) SetDark(dark bool) {
	t.h.dark = dark
	t.rt.themeChanged()
	t.settle()
}

// Frame renders another frame, as when the view's state changed.
func (t *Tester) Frame() { t.h.requested = true; t.settle() }

// Image returns the last frame.
func (t *Tester) Image() *image.RGBA { return t.h.image() }

// Find returns the box of the first element showing text s, or labeled s.
func (t *Tester) Find(s string) (Rect, bool) {
	for _, n := range t.rt.labels {
		if n.text == s {
			return n.r, true
		}
	}
	return Rect{}, false
}

// Texts returns the texts of the last frame, in order.
func (t *Tester) Texts() []string {
	var out []string
	for _, n := range t.rt.labels {
		out = append(out, n.text)
	}
	return out
}

// HasText reports whether the last frame shows a text containing s.
func (t *Tester) HasText(s string) bool {
	for _, n := range t.rt.labels {
		if strings.Contains(n.text, s) {
			return true
		}
	}
	return false
}

// Click clicks the center of the element showing text or labeled s.
func (t *Tester) Click(s string) error {
	r, ok := t.Find(s)
	if !ok {
		return fmt.Errorf("ui: no element shows %q", s)
	}
	t.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	return nil
}

// ClickAt clicks at (x, y), in DIPs.
func (t *Tester) ClickAt(x, y float32) {
	t.send(platform.SurfaceEvent{Kind: platform.PointerDown, X: float64(x), Y: float64(y)})
	t.send(platform.SurfaceEvent{Kind: platform.PointerUp, X: float64(x), Y: float64(y)})
}

// RightClick clicks the center of the element showing text or labeled s
// with the secondary button.
func (t *Tester) RightClick(s string) error {
	r, ok := t.Find(s)
	if !ok {
		return fmt.Errorf("ui: no element shows %q", s)
	}
	t.RightClickAt(r.X+r.W/2, r.Y+r.H/2)
	return nil
}

// RightClickAt clicks at (x, y), in DIPs, with the secondary button.
func (t *Tester) RightClickAt(x, y float32) {
	t.send(platform.SurfaceEvent{Kind: platform.PointerDown, X: float64(x), Y: float64(y), Button: 1})
	t.send(platform.SurfaceEvent{Kind: platform.PointerUp, X: float64(x), Y: float64(y), Button: 1})
}

// Menu returns the labels of the items of the context menu shown, "-" for
// separators, or nil when none is.
func (t *Tester) Menu() []string {
	if t.h.menu == nil {
		return nil
	}
	out := []string{}
	for _, it := range t.h.menu.Items {
		label := it.Label
		if it.Type == platform.MenuItemSeparator {
			label = "-"
		}
		out = append(out, label)
	}
	return out
}

// ChooseMenuItem chooses the item of the context menu shown labeled with
// the last of path, in the submenus the others label, and closes the menu.
func (t *Tester) ChooseMenuItem(path ...string) error {
	m := t.h.menu
	if m == nil {
		return fmt.Errorf("ui: no context menu is shown")
	}
	if len(path) == 0 {
		return fmt.Errorf("ui: no item to choose")
	}
	var found *platform.MenuItem
	for i, label := range path {
		found = nil
		for _, it := range m.Items {
			if it.Label == label && it.Type != platform.MenuItemSeparator {
				found = it
				break
			}
		}
		switch {
		case found == nil:
			return fmt.Errorf("ui: the context menu has no item %q", strings.Join(path[:i+1], " > "))
		case !found.Enabled:
			return fmt.Errorf("ui: the item %q is disabled", strings.Join(path[:i+1], " > "))
		case i < len(path)-1:
			if found.Submenu == nil {
				return fmt.Errorf("ui: the item %q has no submenu", strings.Join(path[:i+1], " > "))
			}
			m = found.Submenu
		}
	}
	if found.Type == platform.MenuItemSubmenu {
		return fmt.Errorf("ui: %q opens a submenu", strings.Join(path, " > "))
	}
	chosen := t.h.chosen
	t.h.menu, t.h.chosen = nil, nil
	chosen(found.ID)
	t.settle()
	return nil
}

// CloseMenu closes the context menu shown without choosing an item.
func (t *Tester) CloseMenu() { t.h.menu, t.h.chosen = nil, nil }

// Press presses (x, y) without releasing; Release releases it.
func (t *Tester) Press(x, y float32) {
	t.send(platform.SurfaceEvent{Kind: platform.PointerDown, X: float64(x), Y: float64(y)})
}

// Release releases the pointer at (x, y).
func (t *Tester) Release(x, y float32) {
	t.send(platform.SurfaceEvent{Kind: platform.PointerUp, X: float64(x), Y: float64(y)})
}

// Move moves the pointer to (x, y).
func (t *Tester) Move(x, y float32) {
	t.send(platform.SurfaceEvent{Kind: platform.PointerMove, X: float64(x), Y: float64(y)})
}

// Scroll scrolls by dx, dy DIPs with the pointer at (x, y).
func (t *Tester) Scroll(x, y, dx, dy float32) {
	t.send(platform.SurfaceEvent{Kind: platform.PointerScroll, X: float64(x), Y: float64(y), DX: float64(dx), DY: float64(dy)})
}

// Key presses a key with modifiers.
func (t *Tester) Key(mods Modifiers, key Key) {
	t.send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: platform.Key(key), Mods: platform.Modifiers(mods)})
	t.send(platform.SurfaceEvent{Kind: platform.KeyReleased, Key: platform.Key(key), Mods: platform.Modifiers(mods)})
}

// Type types text into the focused text input.
func (t *Tester) Type(s string) {
	t.send(platform.SurfaceEvent{Kind: platform.TextInput, Text: s})
}

// Clipboard returns the text the view copied; SetClipboard sets the text
// it pastes.
func (t *Tester) Clipboard() string { return t.h.clipboard }

// SetClipboard sets the clipboard's text.
func (t *Tester) SetClipboard(s string) { t.h.clipboard = s }

// Cursor returns the pointer shape the view shows.
func (t *Tester) Cursor() Cursor { return t.h.cursor }

// OpenedURLs returns the links the view opened.
func (t *Tester) OpenedURLs() []string { return t.h.opened }

// Focused reports whether the element showing text or labeled s, or its
// container, has the keyboard focus.
func (t *Tester) Focused(s string) bool {
	for _, n := range t.rt.labels {
		if n.text != s {
			continue
		}
		for id := n.id; id != 0; {
			if id == t.rt.focused {
				return true
			}
			st := t.rt.states[id]
			if st == nil {
				break
			}
			id = st.parent
		}
	}
	return false
}
