package tooltip

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// Action is a tooltip-aware replacement for [widget.ToolbarAction]. It
// implements [widget.ToolbarItem] so it can be passed directly to
// [widget.NewToolbar].
//
// Internally it renders a low-importance icon button identical to
// Fyne's stock ToolbarAction but wrapped in a [HoverTip] so the icon
// sprouts a help bubble after a brief hover.
type Action struct {
	Icon        fyne.Resource
	Text        string
	OnActivated func()

	btn *widget.Button
	tip *HoverTip
}

// NewAction returns a tooltip-equipped toolbar action.
func NewAction(icon fyne.Resource, text string, onActivated func()) *Action {
	return &Action{Icon: icon, Text: text, OnActivated: onActivated}
}

// ToolbarObject implements [widget.ToolbarItem]. It builds the button +
// tooltip wrapper on first call and reuses them on subsequent calls so
// the toolbar can refresh without losing state.
func (a *Action) ToolbarObject() fyne.CanvasObject {
	if a.btn == nil {
		a.btn = widget.NewButtonWithIcon("", a.Icon, a.OnActivated)
		a.btn.Importance = widget.LowImportance
	} else {
		a.btn.SetIcon(a.Icon)
		a.btn.OnTapped = a.OnActivated
	}
	if a.tip == nil {
		a.tip = New(a.btn, a.Text)
	} else {
		a.tip.SetText(a.Text)
	}
	return a.tip
}

// SetIcon updates the toolbar action's icon at runtime.
func (a *Action) SetIcon(icon fyne.Resource) {
	a.Icon = icon
	if a.btn != nil {
		a.btn.SetIcon(icon)
	}
}

// SetText updates the tooltip text shown on hover.
func (a *Action) SetText(text string) {
	a.Text = text
	if a.tip != nil {
		a.tip.SetText(text)
	}
}

// Enable enables the underlying button.
func (a *Action) Enable() {
	if a.btn != nil {
		a.btn.Enable()
	}
}

// Disable disables the underlying button (the tooltip still appears on
// hover, which is the intended behaviour for "this is greyed out
// because…" affordances).
func (a *Action) Disable() {
	if a.btn != nil {
		a.btn.Disable()
	}
}

// Disabled reports whether the underlying button is disabled.
func (a *Action) Disabled() bool {
	if a.btn == nil {
		return false
	}
	return a.btn.Disabled()
}

// WrapButton attaches a hover tooltip to btn and returns a CanvasObject
// suitable for direct placement in containers. The returned object is
// the [HoverTip] wrapper, so layout sizes match the inner button.
func WrapButton(btn *widget.Button, text string) fyne.CanvasObject {
	return New(btn, text)
}

// Wrap is a generic helper for any CanvasObject that needs hover
// help text (labels, custom widgets, etc.). The wrapped object only
// receives meaningful tooltips when the inner object also implements
// [desktop.Hoverable] — fyne dispatches hover events to the topmost
// Hoverable, which after wrapping is the [HoverTip] itself.
func Wrap(obj fyne.CanvasObject, text string) fyne.CanvasObject {
	return New(obj, text)
}
