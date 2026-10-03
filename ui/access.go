package ui

import (
	"strings"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// Assistive technology, such as screen readers, sees the elements of each
// frame as a tree once it asks for the window: widgets, texts, and the
// elements that take the focus or have a Label, with their states. It acts
// on them as the keyboard and the pointer would: pressing clicks, setting
// a text field's value edits it.

// Role is what an element is to assistive technology. Widgets set their
// own; other elements get one from what they do: one that is clickable and
// takes the focus is a button, a text is a text, a scroll container a
// scroll area, and one with a Label, or that takes the focus, a group.
type Role uint8

const (
	// RoleAuto finds the role from what the element does.
	RoleAuto Role = iota
	// RoleNone leaves the element out, but not its children.
	RoleNone
	RoleGroup
	RoleText
	RoleButton
	RoleLink
	RoleCheckBox
	RoleRadio
	RoleSwitch
	RoleSlider
	RoleProgress
	RoleTextField
	RoleImage
	RoleList
	RoleScroll
	RoleDialog
	RolePopup
	RoleTooltip
	RolePopUpButton
	RoleTabList
	RoleTab
	RoleSplitter
	RoleStatus
	RoleTable
	RoleRow
	RoleCell
	RoleColumnHeader
	RoleTree
	RoleTreeItem
)

// Role sets what the element is to assistive technology, for an element
// drawn as a widget it is not built from, such as a custom toggle.
func (e *Element) Role(r Role) *Element {
	e.role = r
	return e
}

// accessRole returns the element's role, and false for elements assistive
// technology does not see.
func (e *Element) accessRole() (platform.AccessRole, bool) {
	switch e.role {
	case RoleNone:
		return 0, false
	case RoleAuto:
	default:
		return platform.AccessRole(e.role - RoleGroup), true
	}
	switch {
	case e.flags&flagEditable != 0:
		return platform.RoleTextField, true
	case e.kind == kindText:
		return platform.RoleText, e.text != ""
	case e.kind == kindImage || e.kind == kindIcon:
		return platform.RoleImage, e.label != "" // others are decoration
	case e.flags&flagClickable != 0 && e.flags&flagFocusable != 0:
		return platform.RoleButton, true
	case e.flags&(flagScrollX|flagScrollY) != 0:
		return platform.RoleScroll, true
	case e.label != "" || e.flags&flagFocusable != 0:
		return platform.RoleGroup, true
	}
	return 0, false
}

// leafRole reports whether elements of a role take the text inside them
// as their name, rather than showing it as elements of its own.
func leafRole(r platform.AccessRole) bool {
	switch r {
	case platform.RoleGroup, platform.RoleList, platform.RoleScroll, platform.RoleDialog, platform.RolePopup,
		platform.RoleTabList, platform.RoleTable, platform.RoleRow, platform.RoleCell, platform.RoleTree:
		return false
	}
	return true
}

// innerText returns the texts of an element and those inside it, joined
// by spaces.
func (e *Element) innerText() string {
	var b strings.Builder
	var walk func(e *Element)
	walk = func(e *Element) {
		if e.kind == kindText && e.text != "" {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(e.text)
		}
		for ch := e.first; ch != nil; ch = ch.next {
			walk(ch)
		}
	}
	walk(e)
	return b.String()
}

// accessTree describes the last frame for assistive technology.
func (rt *engine) accessTree() *platform.AccessTree {
	t := &platform.AccessTree{}
	if root := rt.c.root; root != nil {
		rt.accessElement(t, root, -1)
	}
	if rt.windowFocused && rt.focused != 0 {
		for _, n := range t.Nodes {
			if n.ID == rt.focused {
				t.Focus = n.ID
				break
			}
		}
	}
	return t
}

func (rt *engine) accessElement(t *platform.AccessTree, e *Element, parent int) {
	if role, ok := e.accessRole(); ok {
		n := platform.AccessNode{
			ID: e.id, Parent: parent, Role: role, Label: e.label,
			Bounds: platform.RectF{X: float64(e.x), Y: float64(e.y), W: float64(e.w), H: float64(e.h)},
		}
		rt.accessDetails(e, &n)
		t.Nodes = append(t.Nodes, n)
		if leafRole(role) {
			return
		}
		parent = len(t.Nodes) - 1
	}
	// Children in flow first, absolute ones above them, as they paint.
	for ch := e.first; ch != nil; ch = ch.next {
		if ch.flags&flagAbsolute == 0 {
			rt.accessElement(t, ch, parent)
		}
	}
	for ch := e.first; ch != nil; ch = ch.next {
		if ch.flags&flagAbsolute != 0 {
			rt.accessElement(t, ch, parent)
		}
	}
}

// accessDetails fills in the name, value, states and actions of a node.
func (rt *engine) accessDetails(e *Element, n *platform.AccessNode) {
	disabled := e.IsDisabled()
	if disabled {
		n.States |= platform.AccessDisabled
	}
	if n.Label == "" && leafRole(n.Role) {
		n.Label = e.innerText()
	}
	switch e.checked {
	case 2:
		n.States |= platform.AccessChecked
	case 3:
		n.States |= platform.AccessMixed
	}
	if e.expanded {
		n.States |= platform.AccessExpanded
	}
	n.Value = e.accValue
	if e.hasRange {
		n.Min, n.Max, n.Now = e.accRange[0], e.accRange[1], e.accRange[2]
	}
	if disabled {
		return
	}
	if e.flags&(flagFocusable|flagEditable) != 0 {
		n.States |= platform.AccessFocusable
		n.Actions |= platform.ActionFocus
	}
	if e.flags&flagClickable != 0 {
		n.Actions |= platform.ActionPress
	}
	if n.Role == platform.RoleSlider {
		n.Actions |= platform.ActionIncrement | platform.ActionDecrement
	}
	if ed := e.st.editor; ed != nil && e.flags&flagEditable != 0 {
		n.Actions |= platform.ActionSetValue
		if ed.multiline {
			n.States |= platform.AccessMultiline
		}
		n.Placeholder = ed.placeholder
		if ed.password {
			n.States |= platform.AccessPassword
		} else {
			n.Value = string(ed.text)
			n.SelStart, n.SelEnd = ed.selection()
		}
	}
}

// accessibilityOn starts describing frames for assistive technology,
// with the last one at once.
func (rt *engine) accessibilityOn() {
	if !rt.access {
		rt.access = true
		if rt.c.root == nil {
			rt.requestFrame()
		}
	}
	rt.host.updateAccessibility(rt.accessTree())
}

// accessAction performs an action of assistive technology on an element
// of the last frame, as the keyboard or the pointer would.
func (rt *engine) accessAction(ev platform.SurfaceEvent) {
	s := rt.states[ev.ID]
	if s == nil || s.flags&flagDisabled != 0 {
		return
	}
	switch ev.Action {
	case platform.AccessPress:
		if s.flags&flagClickable != 0 {
			s.clicks++
		} else {
			rt.focusOn(s)
		}
	case platform.AccessFocus:
		rt.focusOn(s)
	case platform.AccessIncrement, platform.AccessDecrement:
		rt.focusOn(s)
		k := KeyRight
		if ev.Action == platform.AccessDecrement {
			k = KeyLeft
		}
		rt.keys = append(rt.keys, keyEvent{0, k})
	case platform.AccessSetValue:
		if s.editor == nil || s.flags&flagEditable == 0 {
			return
		}
		rt.focusOn(s)
		s.editor.queue = append(s.editor.queue, editEvent{kind: editCommand, text: "selectAll"}, editEvent{kind: editInsert, text: ev.Text})
	}
	rt.requestFrame()
}

// focusOn gives an element the keyboard focus, as Tab would.
func (rt *engine) focusOn(s *state) {
	if s.flags&(flagFocusable|flagEditable) == 0 {
		return
	}
	if rt.focused != s.id {
		rt.focused = s.id
		rt.blinkStart = time.Now()
	}
	rt.focusVisible = true
	rt.scrollIntoView(s.id)
}
