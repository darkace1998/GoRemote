// Package tooltip provides a small in-tree tooltip helper for the Fyne
// desktop UI. Fyne v2.7 has no built-in tooltip widget; this package
// wraps an inner CanvasObject in a [HoverTip] that listens for hover
// events on the desktop driver and displays a short text bubble in a
// non-modal pop-up after a brief delay.
//
// The wrapper also forwards primary/secondary tap events and hover
// callbacks to the inner widget when the inner widget implements the
// matching Fyne interface, so wrapping a [*widget.Button] keeps it
// clickable and keeps its hover highlight.
package tooltip

import (
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// ShowDelay is the dwell time before a tooltip is displayed. It is a
// variable so tests can shorten it without touching production timing.
var ShowDelay = 600 * time.Millisecond

// cursorOffset is how far below/right of the hover position the tooltip
// is anchored. Keeps it from sitting under the pointer.
const (
	cursorOffsetX float32 = 12
	cursorOffsetY float32 = 18
)

// HoverTip wraps a single inner [fyne.CanvasObject] and shows a tooltip
// pop-up containing its Text after a short hover. The tooltip is hidden
// on MouseOut, on Tap, or when the widget is hidden.
type HoverTip struct {
	widget.BaseWidget

	mu      sync.Mutex
	text    string
	inner   fyne.CanvasObject
	timer   *time.Timer
	popup   *widget.PopUp
	lastPos fyne.Position
}

// New wraps obj with a tooltip showing text on hover. If text is empty
// the tooltip is suppressed but the wrapper is still safe to use as a
// pass-through. obj must not be nil.
func New(obj fyne.CanvasObject, text string) *HoverTip {
	if obj == nil {
		panic("tooltip.New: inner CanvasObject must not be nil")
	}
	h := &HoverTip{text: text, inner: obj}
	h.ExtendBaseWidget(h)
	return h
}

// SetText updates the tooltip text. Safe to call from the UI thread.
// If a tooltip is currently visible it is left unchanged; the new text
// takes effect on the next hover.
func (h *HoverTip) SetText(text string) {
	h.mu.Lock()
	h.text = text
	h.mu.Unlock()
}

// Text returns the current tooltip text.
func (h *HoverTip) Text() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.text
}

// CreateRenderer implements fyne.Widget.
func (h *HoverTip) CreateRenderer() fyne.WidgetRenderer {
	return &hoverTipRenderer{h: h}
}

// MouseIn schedules a tooltip and forwards the event to the inner
// widget if it is also Hoverable (so e.g. Button hover highlight works).
func (h *HoverTip) MouseIn(e *desktop.MouseEvent) {
	if e != nil {
		h.scheduleShow(e.Position)
	} else {
		h.scheduleShow(fyne.NewPos(0, 0))
	}
	if hov, ok := h.inner.(desktop.Hoverable); ok {
		hov.MouseIn(e)
	}
}

// MouseMoved tracks the latest pointer position so the tooltip can be
// anchored near the cursor and forwards to inner if applicable.
func (h *HoverTip) MouseMoved(e *desktop.MouseEvent) {
	if e != nil {
		h.mu.Lock()
		h.lastPos = e.Position
		h.mu.Unlock()
	}
	if hov, ok := h.inner.(desktop.Hoverable); ok {
		hov.MouseMoved(e)
	}
}

// MouseOut cancels any pending tooltip and hides a visible one.
func (h *HoverTip) MouseOut() {
	h.cancelAndHide()
	if hov, ok := h.inner.(desktop.Hoverable); ok {
		hov.MouseOut()
	}
}

// Tapped forwards primary taps to the inner widget when supported and
// dismisses any visible tooltip first.
func (h *HoverTip) Tapped(e *fyne.PointEvent) {
	h.cancelAndHide()
	if t, ok := h.inner.(fyne.Tappable); ok {
		t.Tapped(e)
	}
}

// TappedSecondary forwards secondary taps to the inner widget when
// supported and dismisses any visible tooltip first.
func (h *HoverTip) TappedSecondary(e *fyne.PointEvent) {
	h.cancelAndHide()
	if t, ok := h.inner.(fyne.SecondaryTappable); ok {
		t.TappedSecondary(e)
	}
}

// Hide ensures any visible tooltip is dismissed before hiding.
func (h *HoverTip) Hide() {
	h.cancelAndHide()
	h.BaseWidget.Hide()
}

func (h *HoverTip) scheduleShow(pos fyne.Position) {
	h.mu.Lock()
	h.lastPos = pos
	if h.text == "" {
		h.mu.Unlock()
		return
	}
	if h.timer != nil {
		h.timer.Stop()
	}
	delay := ShowDelay
	h.timer = time.AfterFunc(delay, func() {
		fyne.Do(h.show)
	})
	h.mu.Unlock()
}

// show is invoked on the UI thread once the dwell timer fires.
func (h *HoverTip) show() {
	h.mu.Lock()
	text := h.text
	pos := h.lastPos
	prev := h.popup
	h.popup = nil
	h.mu.Unlock()
	if prev != nil {
		prev.Hide()
	}
	if text == "" {
		return
	}
	app := fyne.CurrentApp()
	if app == nil {
		return
	}
	drv := app.Driver()
	if drv == nil {
		return
	}
	cnv := drv.CanvasForObject(h)
	if cnv == nil {
		return
	}
	label := widget.NewLabel(text)
	pop := widget.NewPopUp(label, cnv)
	pop.ShowAtRelativePosition(fyne.NewPos(pos.X+cursorOffsetX, pos.Y+cursorOffsetY), h)
	h.mu.Lock()
	h.popup = pop
	h.mu.Unlock()
}

// cancelAndHide stops a pending dwell timer and hides any visible popup.
// Safe to call repeatedly.
func (h *HoverTip) cancelAndHide() {
	h.mu.Lock()
	if h.timer != nil {
		h.timer.Stop()
		h.timer = nil
	}
	pop := h.popup
	h.popup = nil
	h.mu.Unlock()
	if pop != nil {
		pop.Hide()
	}
}

// --- renderer --------------------------------------------------------------

type hoverTipRenderer struct {
	h *HoverTip
}

func (r *hoverTipRenderer) Layout(size fyne.Size) {
	r.h.inner.Resize(size)
	r.h.inner.Move(fyne.NewPos(0, 0))
}

func (r *hoverTipRenderer) MinSize() fyne.Size {
	return r.h.inner.MinSize()
}

func (r *hoverTipRenderer) Refresh() {
	r.h.inner.Refresh()
}

func (r *hoverTipRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.h.inner}
}

func (r *hoverTipRenderer) Destroy() {}
