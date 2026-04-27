package workspace

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	t.Parallel()
	d := Default()
	if d.Version != CurrentVersion {
		t.Errorf("Version = %d, want %d", d.Version, CurrentVersion)
	}
	if d.OpenTabs == nil {
		t.Error("OpenTabs is nil; expected empty slice")
	}
	if len(d.OpenTabs) != 0 {
		t.Errorf("OpenTabs len = %d, want 0", len(d.OpenTabs))
	}
	if d.ActiveTab != "" {
		t.Errorf("ActiveTab = %q, want empty", d.ActiveTab)
	}
	if err := d.Validate(); err != nil {
		t.Errorf("Default().Validate() = %v", err)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	tab := func(id string) TabState {
		return TabState{ID: id, ConnectionID: "c-" + id, Title: id}
	}
	cases := []struct {
		name    string
		w       Workspace
		wantSub string
	}{
		{
			name:    "version zero",
			w:       Workspace{Version: 0},
			wantSub: "version",
		},
		{
			name:    "version negative",
			w:       Workspace{Version: -1},
			wantSub: "version",
		},
		{
			name: "empty id",
			w: Workspace{
				Version:  1,
				OpenTabs: []TabState{tab("a"), {ID: "", ConnectionID: "x"}},
			},
			wantSub: "id is empty",
		},
		{
			name: "duplicate ids",
			w: Workspace{
				Version:  1,
				OpenTabs: []TabState{tab("a"), tab("a")},
			},
			wantSub: "duplicate id",
		},
		{
			name: "active tab not in open tabs",
			w: Workspace{
				Version:   1,
				OpenTabs:  []TabState{tab("a")},
				ActiveTab: "b",
			},
			wantSub: "activeTab",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.w.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("Validate() = %v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

func TestValidateOK(t *testing.T) {
	t.Parallel()
	w := Workspace{
		Version: 1,
		OpenTabs: []TabState{
			{ID: "t1", ConnectionID: "c1", Title: "one"},
			{ID: "t2", ConnectionID: "c2", Title: "two"},
		},
		ActiveTab: "t2",
	}
	if err := w.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}

	w.ActiveTab = ""
	if err := w.Validate(); err != nil {
		t.Errorf("Validate() empty active = %v, want nil", err)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	t.Parallel()
	in := Workspace{
		Version: 1,
		OpenTabs: []TabState{
			{
				ID:           "t1",
				ConnectionID: "c1",
				Title:        "Lab Router",
				PaneGroup:    "left",
				Pinned:       true,
				LastUsedAt:   time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC),
			},
		},
		ActiveTab: "t1",
		UpdatedAt: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out Workspace
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(out.OpenTabs) != 1 || out.OpenTabs[0] != in.OpenTabs[0] {
		t.Errorf("tab round trip mismatch:\n got = %+v\nwant = %+v", out.OpenTabs, in.OpenTabs)
	}
	if out.ActiveTab != in.ActiveTab || out.Version != in.Version || !out.UpdatedAt.Equal(in.UpdatedAt) {
		t.Errorf("scalar round trip mismatch:\n got = %+v\nwant = %+v", out, in)
	}
	for _, key := range []string{
		"\"version\"", "\"openTabs\"", "\"activeTab\"", "\"updatedAt\"",
		"\"id\"", "\"connectionId\"", "\"title\"", "\"paneGroup\"",
		"\"pinned\"", "\"lastUsedAt\"",
	} {
		if !strings.Contains(string(data), key) {
			t.Errorf("encoded JSON missing key %s: %s", key, string(data))
		}
	}
}

func TestJSONOmitsOptionalEmpty(t *testing.T) {
	t.Parallel()
	w := Workspace{
		Version:  1,
		OpenTabs: []TabState{{ID: "t1", ConnectionID: "c1", Title: "x"}},
	}
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(data)
	if strings.Contains(s, "\"activeTab\"") {
		t.Errorf("activeTab should be omitted when empty: %s", s)
	}
	if strings.Contains(s, "\"paneGroup\"") {
		t.Errorf("paneGroup should be omitted when empty: %s", s)
	}
	if strings.Contains(s, "\"pinned\"") {
		t.Errorf("pinned should be omitted when false: %s", s)
	}
}
