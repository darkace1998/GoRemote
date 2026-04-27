package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// captureLogger records every error message for assertions.
type captureLogger struct {
	mu       sync.Mutex
	messages []string
}

func (c *captureLogger) Error(msg string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	parts := []string{msg}
	for _, a := range args {
		parts = append(parts, toStr(a))
	}
	c.messages = append(c.messages, strings.Join(parts, " "))
}

func (c *captureLogger) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.messages))
	copy(out, c.messages)
	return out
}

func toStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case error:
		return x.Error()
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func tabA() TabState { return TabState{ID: "a", ConnectionID: "ca", Title: "A"} }
func tabB() TabState { return TabState{ID: "b", ConnectionID: "cb", Title: "B"} }

func TestStore_LoadMissingReturnsDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.json")
	s := NewFileStore(path, nil)

	w, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if w.Version != CurrentVersion || len(w.OpenTabs) != 0 || w.ActiveTab != "" {
		t.Errorf("Load missing = %+v, want Default()", w)
	}
}

func TestStore_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.json")
	s := NewFileStore(path, nil)
	ctx := context.Background()

	in := Workspace{
		Version:   1,
		OpenTabs:  []TabState{tabA(), tabB()},
		ActiveTab: "b",
	}
	if err := s.Save(ctx, in); err != nil {
		t.Fatalf("Save: %v", err)
	}

	out, err := s.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.Version != 1 || len(out.OpenTabs) != 2 || out.ActiveTab != "b" {
		t.Errorf("round trip = %+v", out)
	}
	if out.OpenTabs[0] != in.OpenTabs[0] || out.OpenTabs[1] != in.OpenTabs[1] {
		t.Errorf("tab mismatch: got %+v want %+v", out.OpenTabs, in.OpenTabs)
	}
	if out.UpdatedAt.IsZero() {
		t.Error("UpdatedAt was not stamped on Save")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %v, want 0600", mode)
	}
}

func TestStore_LoadCorruptReturnsDefaultAndLogs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	log := &captureLogger{}
	s := NewFileStore(path, log)

	w, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if w.Version != CurrentVersion || len(w.OpenTabs) != 0 {
		t.Errorf("corrupt load = %+v, want Default()", w)
	}
	msgs := log.all()
	if len(msgs) == 0 || !strings.Contains(strings.Join(msgs, "|"), "corrupt") {
		t.Errorf("expected corrupt log message; got %v", msgs)
	}
}

func TestStore_LoadInvalidReturnsDefaultAndLogs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.json")
	bad := Workspace{Version: 1, OpenTabs: []TabState{tabA(), tabA()}}
	data, _ := json.Marshal(bad)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	log := &captureLogger{}
	s := NewFileStore(path, log)

	w, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(w.OpenTabs) != 0 {
		t.Errorf("invalid load = %+v, want Default()", w)
	}
	msgs := log.all()
	if len(msgs) == 0 || !strings.Contains(strings.Join(msgs, "|"), "invalid") {
		t.Errorf("expected invalid log message; got %v", msgs)
	}
}

func TestStore_SaveRejectsInvalid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.json")
	s := NewFileStore(path, nil)

	bad := Workspace{Version: 1, OpenTabs: []TabState{tabA()}, ActiveTab: "missing"}
	if err := s.Save(context.Background(), bad); err == nil {
		t.Fatal("Save invalid = nil; want error")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file should not exist after rejected Save: stat err = %v", err)
	}
}

func TestStore_AtomicLeavesNoPartials(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.json")
	s := NewFileStore(path, nil)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		w := Workspace{
			Version:  1,
			OpenTabs: []TabState{{ID: "t", ConnectionID: "c", Title: "x"}},
		}
		if err := s.Save(ctx, w); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "workspace.json" {
			t.Errorf("unexpected leftover file: %q", e.Name())
		}
	}
}

func TestStore_ConcurrentSaves(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.json")
	s := NewFileStore(path, nil)
	ctx := context.Background()

	if err := s.Save(ctx, Workspace{Version: 1, OpenTabs: []TabState{tabA()}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const N = 30
	var wg sync.WaitGroup
	var failures atomic.Int32
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := Workspace{
				Version: 1,
				OpenTabs: []TabState{
					{ID: "a", ConnectionID: "ca", Title: "A"},
					{ID: "b", ConnectionID: "cb", Title: "B"},
				},
				ActiveTab: "a",
			}
			if i%2 == 0 {
				w.ActiveTab = "b"
			}
			if err := s.Save(ctx, w); err != nil {
				failures.Add(1)
				t.Errorf("Save %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if failures.Load() > 0 {
		t.Fatalf("%d concurrent Save failures", failures.Load())
	}

	out, err := s.Load(ctx)
	if err != nil {
		t.Fatalf("Load after concurrent saves: %v", err)
	}
	if len(out.OpenTabs) != 2 {
		t.Errorf("post-concurrent OpenTabs = %d, want 2", len(out.OpenTabs))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "workspace.json" {
			t.Errorf("leftover file after concurrent saves: %q", e.Name())
		}
	}
}

func TestStore_LoadCtxCancelled(t *testing.T) {
	t.Parallel()
	s := NewFileStore(filepath.Join(t.TempDir(), "w.json"), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w, err := s.Load(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if w.Version != CurrentVersion {
		t.Errorf("Load on cancelled ctx returned %+v", w)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Parallel()
	p, err := DefaultPath()
	if err != nil {
		t.Skipf("UserConfigDir unavailable: %v", err)
	}
	if !strings.HasSuffix(p, filepath.Join("goremote", "workspace.json")) {
		t.Errorf("DefaultPath = %q, want suffix goremote/workspace.json", p)
	}
}

func TestStore_SaveStampsUpdatedAt(t *testing.T) {
	t.Parallel()
	s := NewFileStore(filepath.Join(t.TempDir(), "w.json"), nil)
	ctx := context.Background()
	w := Workspace{Version: 1, OpenTabs: []TabState{tabA()}}

	if err := s.Save(ctx, w); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	first, _ := s.Load(ctx)
	time.Sleep(2 * time.Millisecond)
	if err := s.Save(ctx, w); err != nil {
		t.Fatalf("save 2: %v", err)
	}
	second, _ := s.Load(ctx)
	if !second.UpdatedAt.After(first.UpdatedAt) {
		t.Errorf("UpdatedAt did not advance: first=%v second=%v", first.UpdatedAt, second.UpdatedAt)
	}
}
