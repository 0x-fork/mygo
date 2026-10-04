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
	// Description tells more about the element than its name, as help
	// text read after it: a field's description and its error, or a
	// tooltip.
	Description string
	// PosInSet is the place of an item in its set, from 1, and SetSize
	// the size of the set, 0 when not given: a list's rows say which of
	// all they are, whether or not the others are built, and the list how
	// many rows it has.
	PosInSet, SetSize int
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
	RoleListItem
	RoleMenuButton
	RoleToolbar
	RoleRadioGroup
	// RoleToggleButton is pressed while AccessChecked.
	RoleToggleButton
	// RoleComboBox is a text field with a popup of options, which is
	// AccessExpanded while it shows.
	RoleComboBox
	// RoleDisclosure is a button showing or hiding content below it, which
	// is AccessExpanded while it shows: the trigger of a collapsible, or
	// the header of an accordion's section.
	RoleDisclosure
)

// AccessStates are the states of an element of an AccessTree.
// AccessChecked is the choice among a tab list's tabs, a tree's items, and
// the rows of a list or a table that are AccessSelectable.
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
	AccessSelectable
	// AccessOffscreen is set on elements built but out of view, as the
	// rows a list builds beyond its edges.
	AccessOffscreen
	// AccessMultiselectable is set on a list or a table whose user
	// chooses several rows.
	AccessMultiselectable
	// AccessSegment is set on the segments of a segmented control or a
	// group of toggles, as AppKit's subrole AXSegment says.
	AccessSegment
	// AccessSearch is set on a text field for searching.
	AccessSearch
	// AccessInvalid is set on a control whose value is not valid, as a
	// field with an error.
	AccessInvalid
)

// AccessActions are the actions an element of an AccessTree takes.
type AccessActions uint8

const (
	ActionPress AccessActions = 1 << iota
	ActionFocus
	ActionIncrement
	ActionDecrement
	ActionSetValue
	// ActionScrollIntoView scrolls the containers around the element to
	// show it, as assistive technology moving to it out of view asks.
	ActionScrollIntoView
)

// AccessActionKind is the action of an AccessAction event.
type AccessActionKind uint8

const (
	AccessPress AccessActionKind = iota
	AccessFocus
	AccessIncrement
	AccessDecrement
	AccessSetValue
	AccessScrollIntoView
)
