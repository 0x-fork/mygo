package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// node returns the node of the tree with role and a label containing
// label.
func node(t *testing.T, tree *platform.AccessTree, role platform.AccessRole, label string) platform.AccessNode {
	t.Helper()
	for _, n := range tree.Nodes {
		if n.Role == role && strings.Contains(n.Label, label) {
			return n
		}
	}
	var have []string
	for _, n := range tree.Nodes {
		have = append(have, n.Label)
	}
	t.Fatalf("no node of role %d labeled %q among %q", role, label, have)
	return platform.AccessNode{}
}

func TestAccessibilityTree(t *testing.T) {
	d := &demo{name: "Ada", volume: 30}
	tt := NewTester(d.view, 640, 600)
	if tt.h.access != nil {
		t.Fatal("a tree before assistive technology asked")
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	tree := tt.h.access
	if tree == nil || len(tree.Nodes) == 0 {
		t.Fatal("no tree after AccessibilityOn")
	}
	inc := node(t, tree, platform.RoleButton, "Increment")
	if inc.Actions&platform.ActionPress == 0 || inc.Bounds.W <= 0 {
		t.Errorf("Increment: %+v", inc)
	}
	node(t, tree, platform.RoleText, "MyGo UI")
	agree := node(t, tree, platform.RoleCheckBox, "I agree")
	if agree.States&platform.AccessChecked != 0 {
		t.Error("the check box is checked")
	}
	node(t, tree, platform.RoleSwitch, "")
	node(t, tree, platform.RoleRadio, "Alpha")
	slider := node(t, tree, platform.RoleSlider, "")
	if slider.Min != 0 || slider.Max != 100 || slider.Now != 30 {
		t.Errorf("slider range %v..%v at %v", slider.Min, slider.Max, slider.Now)
	}
	field := node(t, tree, platform.RoleTextField, "")
	if field.Value != "Ada" || field.Actions&platform.ActionSetValue == 0 {
		t.Errorf("text field: %+v", field)
	}
	node(t, tree, platform.RolePopUpButton, "")
	list := node(t, tree, platform.RoleList, "")
	row := node(t, tree, platform.RoleText, "Row 3")
	for p := row.Parent; ; p = tree.Nodes[p].Parent {
		if p < 0 {
			t.Fatal("the row is not inside the list")
		}
		if tree.Nodes[p].ID == list.ID {
			break
		}
	}
	// Buttons name themselves after their text, which is not a node.
	for _, n := range tree.Nodes {
		if n.Role == platform.RoleText && n.Label == "Increment" {
			t.Error("the button's text is a node of its own")
		}
	}

	// Actions act as the pointer and the keyboard would.
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: inc.ID, Action: platform.AccessPress})
	if d.count != 1 {
		t.Errorf("pressing Increment: count %d", d.count)
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: agree.ID, Action: platform.AccessPress})
	if !d.agree {
		t.Error("pressing the check box did not check it")
	}
	if n := node(t, tt.h.access, platform.RoleCheckBox, "I agree"); n.States&platform.AccessChecked == 0 {
		t.Error("the tree after the frame shows the check box unchecked")
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: slider.ID, Action: platform.AccessIncrement})
	if d.volume <= 30 {
		t.Errorf("incrementing the slider: %v", d.volume)
	}
	if tt.h.access.Focus != slider.ID {
		t.Error("the slider has no focus after incrementing it")
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: field.ID, Action: platform.AccessSetValue, Text: "Grace"})
	if d.name != "Grace" {
		t.Errorf("setting the text field: %q", d.name)
	}
	if f := node(t, tt.h.access, platform.RoleTextField, ""); f.Value != "Grace" || f.SelStart != 5 || f.SelEnd != 5 || tt.h.access.Focus != f.ID {
		t.Errorf("text field after setting it: %+v, focus %d", f, tt.h.access.Focus)
	}
}

func TestAccessibilityOfOverlays(t *testing.T) {
	open := true
	tt := NewTester(func(c *Context) {
		Button(c, "Behind")
		Modal(c, &open, func() {
			Text(c, "Sure?")
			Button(c, "OK")
		})
	}, 400, 300)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	tree := tt.h.access
	dialog := node(t, tree, platform.RoleDialog, "")
	ok := node(t, tree, platform.RoleButton, "OK")
	if tree.Nodes[ok.Parent].ID != dialog.ID {
		t.Error("the dialog's button is not inside it")
	}
}

func TestRole(t *testing.T) {
	on := false
	tt := NewTester(func(c *Context) {
		b := Box(c).Size(20, 20).Focusable().Role(RoleSwitch).Label("Wi-Fi")
		if b.Clicked() {
			on = !on
		}
		Box(c).Size(20, 20).Label("hidden").Role(RoleNone)
	}, 100, 100)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	sw := node(t, tt.h.access, platform.RoleSwitch, "Wi-Fi")
	for _, n := range tt.h.access.Nodes {
		if n.Label == "hidden" {
			t.Error("an element of RoleNone is in the tree")
		}
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: sw.ID, Action: platform.AccessPress})
	if !on {
		t.Error("pressing the custom switch did not click it")
	}
}

// TestInputMethodContext checks that input methods see the text around
// the caret and replace what they typed, as macOS's press and hold does.
func TestInputMethodContext(t *testing.T) {
	d := &demo{name: "caf"}
	tt := NewTester(d.view, 640, 600)
	r, _ := tt.Find("I agree")
	tt.ClickAt(r.X+20, r.Y+40+r.H/2)
	tt.Key(0, KeyEnd)
	ime := tt.h.ime
	if !ime.Active || ime.Text != "caf" || ime.Start != 3 || ime.End != 3 {
		t.Fatalf("text input state %+v", ime)
	}
	// Typing e, then holding it: the input method composes over the e
	// it typed, then commits the accented letter in its place.
	tt.Type("e")
	if tt.h.ime.Text != "cafe" {
		t.Fatalf("after typing: %+v", tt.h.ime)
	}
	tt.send(platform.SurfaceEvent{Kind: platform.TextComposition, Text: "e", Caret: 1, Replace: true, From: 3, To: 4})
	if d.name != "caf" {
		t.Errorf("composing over the e: %q", d.name)
	}
	tt.send(platform.SurfaceEvent{Kind: platform.TextInput, Text: "é"})
	if d.name != "café" {
		t.Errorf("committing: %q", d.name)
	}
	// Replacements count from the start of the text the input method got,
	// which ends imeContext runes before the selection in long texts.
	d.name = strings.Repeat("x", 2*imeContext) + "ab"
	tt.Frame()
	tt.Key(0, KeyEnd)
	ime = tt.h.ime
	if len([]rune(ime.Text)) != imeContext || ime.Start != imeContext || !strings.HasSuffix(ime.Text, "xab") {
		t.Fatalf("long text: %d runes, selection %d", len([]rune(ime.Text)), ime.Start)
	}
	tt.send(platform.SurfaceEvent{Kind: platform.TextInput, Text: "B", Replace: true, From: imeContext - 1, To: imeContext})
	if !strings.HasSuffix(d.name, "xaB") {
		t.Errorf("replacing in a long text: ...%q", d.name[len(d.name)-5:])
	}
}
