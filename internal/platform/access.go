package platform

// AccessTree is the content of a surface for assistive technology, such as
// screen readers: the elements a user can perceive and act on.
type AccessTree struct {
	// Nodes are the elements in tree order: a parent before its children.
	Nodes []AccessNode
	// Focus is the ID of the element with the keyboard focus, 0 for none.
	Focus uint64
}

// AccessNode is an element of an AccessTree.
type AccessNode struct {
	// ID identifies the element from frame to frame.
	ID uint64
	// Parent is the index of the parent node in AccessTree.Nodes, -1 for
	// the elements at the top.
	Parent int
	Role   AccessRole
	// Label names the element; Value is its value, as the text of a text
	// field, for elements that have one.
	Label, Value string
	// Bounds is the element's visible box, in DIPs relative to the
	// surface.
	Bounds RectF
	States AccessStates
	// Min, Max and Now are the range and the value of a slider or a
	// progress bar; Now is below Min for progress of unknown length.
	Min, Max, Now float64
	// SelStart and SelEnd are the selection of a text field, in runes,
	// and Placeholder what it shows while empty.
	SelStart, SelEnd int
	Placeholder      string
	// Actions are the actions the element takes in AccessAction events.
	Actions AccessActions
}

// AccessRole is the kind of an element of an AccessTree.
type AccessRole uint8

const (
	RoleGroup AccessRole = iota
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

// AccessStates are the states of an element of an AccessTree.
type AccessStates uint16

const (
	AccessFocusable AccessStates = 1 << iota
	AccessDisabled
	AccessChecked
	AccessMixed
	AccessSelected
	AccessExpanded
	AccessMultiline
	AccessPassword
	AccessReadOnly
)

// AccessActions are the actions an element of an AccessTree takes.
type AccessActions uint8

const (
	ActionPress AccessActions = 1 << iota
	ActionFocus
	ActionIncrement
	ActionDecrement
	ActionSetValue
)

// AccessActionKind is the action of an AccessAction event.
type AccessActionKind uint8

const (
	AccessPress AccessActionKind = iota
	AccessFocus
	AccessIncrement
	AccessDecrement
	AccessSetValue
)
