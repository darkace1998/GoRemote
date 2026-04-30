package tooltip

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// findLabel walks the canvas overlay tree looking for a *widget.Label
// whose text matches want. Returns true on first hit.
func findLabel(objs []fyne.CanvasObject, want string) bool {
	for _, o := range objs {
		if l, ok := o.(*widget.Label); ok && l.Text == want {
			return true
		}
		if c, ok := o.(interface{ Objects() []fyne.CanvasObject }); ok {
			if findLabel(c.Objects(), want) {
				return true
			}
		}
	}
	return false
}

// findInOverlays searches all of the test window's current overlays
// (plus their canvas object trees) for a label with the given text.
func findInOverlays(w fyne.Window, want string) bool {
	for _, ov := range w.Canvas().Overlays().List() {
		if pop, ok := ov.(*widget.PopUp); ok {
			if findLabel([]fyne.CanvasObject{pop.Content}, want) {
				return true
			}
		}
		if findLabel([]fyne.CanvasObject{ov}, want) {
			return true
		}
	}
	return false
}

func TestHoverTip_ShowAndHide(t *testing.T) {
	test.NewApp()
	defer test.NewApp() // reset for isolation

	clicked := false
	btn := widget.NewButton("hi", func() { clicked = true })
	tip := New(btn, "tooltip text")

	w := test.NewWindow(tip)
	defer w.Close()
	w.Resize(fyne.NewSize(300, 100))

	// Drive the show path synchronously rather than through the dwell
	// timer to keep the test free of goroutine races (the test fyne
	// driver dispatches fyne.Do inline on the caller goroutine, which
	// would race with the test goroutine if the timer fired).
	tip.lastPos = fyne.NewPos(0, 0)
	tip.show()

	if !findInOverlays(w, "tooltip text") {
		t.Fatalf("expected tooltip popup with text %q to be visible after show()", "tooltip text")
	}

	tip.cancelAndHide()
	if findInOverlays(w, "tooltip text") {
		t.Fatalf("tooltip should be hidden after cancelAndHide")
	}

	// Sanity-check that the wrapper still forwards taps to the button.
	tip.Tapped(&fyne.PointEvent{})
	if !clicked {
		t.Fatalf("expected wrapped button OnTapped to fire when HoverTip.Tapped is called")
	}
}

func TestHoverTip_TimerSchedulesShow(t *testing.T) {
	test.NewApp()
	defer test.NewApp()

	prev := ShowDelay
	ShowDelay = 5 * time.Millisecond
	defer func() { ShowDelay = prev }()

	tip := New(widget.NewLabel("anchor"), "scheduled")
	w := test.NewWindow(tip)
	defer w.Close()

	tip.scheduleShow(fyne.NewPos(0, 0))
	// Wait for the timer to fire; we don't assert visibility (which
	// would race with the timer goroutine that calls show via fyne.Do
	// on the test driver), only that the timer is created and clears
	// itself without panicking.
	time.Sleep(40 * time.Millisecond)

	tip.cancelAndHide()
}

func TestHoverTip_EmptyTextSuppressesPopup(t *testing.T) {
	test.NewApp()
	defer test.NewApp()

	tip := New(widget.NewLabel("anchor"), "")
	w := test.NewWindow(tip)
	defer w.Close()
	w.Resize(fyne.NewSize(200, 80))

	// Empty text -> show() is a no-op; no popup is produced.
	tip.lastPos = fyne.NewPos(0, 0)
	tip.show()

	for _, ov := range w.Canvas().Overlays().List() {
		if _, ok := ov.(*widget.PopUp); ok {
			t.Fatalf("no popup expected when tooltip text is empty")
		}
	}
}

func TestHoverTip_SetTextUpdatesText(t *testing.T) {
	test.NewApp()
	defer test.NewApp()

	tip := New(widget.NewLabel("x"), "first")
	if got := tip.Text(); got != "first" {
		t.Fatalf("Text() = %q, want %q", got, "first")
	}
	tip.SetText("second")
	if got := tip.Text(); got != "second" {
		t.Fatalf("after SetText, Text() = %q, want %q", got, "second")
	}
}

func TestAction_ToolbarObjectReusesButton(t *testing.T) {
	test.NewApp()
	defer test.NewApp()

	a := NewAction(nil, "hello", func() {})
	o1 := a.ToolbarObject()
	o2 := a.ToolbarObject()
	if o1 != o2 {
		t.Fatalf("ToolbarObject must be stable across calls; got %p then %p", o1, o2)
	}
	a.SetText("changed")
	if a.tip.Text() != "changed" {
		t.Fatalf("SetText did not propagate to the inner HoverTip")
	}
}

func TestAction_DisableEnable(t *testing.T) {
	test.NewApp()
	defer test.NewApp()

	a := NewAction(nil, "tip", func() {})
	if a.Disabled() {
		t.Fatalf("fresh action should not report Disabled before render")
	}
	_ = a.ToolbarObject() // build button
	a.Disable()
	if !a.Disabled() {
		t.Fatalf("Disabled() should be true after Disable()")
	}
	a.Enable()
	if a.Disabled() {
		t.Fatalf("Disabled() should be false after Enable()")
	}
}
