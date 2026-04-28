package workspace

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTouchRecentNewEntry(t *testing.T) {
	w := Default()
	w.TouchRecent("conn-a", time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC))
	if got := len(w.Recents); got != 1 {
		t.Fatalf("len(Recents) = %d, want 1", got)
	}
	if w.Recents[0].ConnectionID != "conn-a" {
		t.Errorf("ConnectionID = %q", w.Recents[0].ConnectionID)
	}
	if w.Recents[0].OpenCount != 1 {
		t.Errorf("OpenCount = %d, want 1", w.Recents[0].OpenCount)
	}
}

func TestTouchRecentMovesExistingToFront(t *testing.T) {
	w := Default()
	w.TouchRecent("a", time.Now())
	w.TouchRecent("b", time.Now())
	w.TouchRecent("a", time.Now())
	if got := w.Recents[0].ConnectionID; got != "a" {
		t.Errorf("front = %q, want a", got)
	}
	if got := w.Recents[0].OpenCount; got != 2 {
		t.Errorf("OpenCount = %d, want 2", got)
	}
	if got := len(w.Recents); got != 2 {
		t.Errorf("len = %d, want 2 (no duplicates)", got)
	}
}

func TestTouchRecentBoundsAtMax(t *testing.T) {
	w := Default()
	for i := 0; i < MaxRecents+5; i++ {
		w.TouchRecent("c"+string(rune('A'+i)), time.Now())
	}
	if got := len(w.Recents); got != MaxRecents {
		t.Errorf("len = %d, want %d", got, MaxRecents)
	}
}

func TestTouchRecentIgnoresEmpty(t *testing.T) {
	w := Default()
	w.TouchRecent("", time.Now())
	if got := len(w.Recents); got != 0 {
		t.Errorf("len = %d, want 0", got)
	}
}

func TestRecentsRoundTripJSON(t *testing.T) {
	w := Default()
	w.TouchRecent("conn-1", time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	w.TouchRecent("conn-2", time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC))
	raw, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Workspace
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Recents) != 2 {
		t.Fatalf("Recents len = %d, want 2", len(got.Recents))
	}
	if got.Recents[0].ConnectionID != "conn-2" {
		t.Errorf("first Recents = %q, want conn-2", got.Recents[0].ConnectionID)
	}
}
