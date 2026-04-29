package persistence

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkace1998/GoRemote/internal/domain"
	"github.com/darkace1998/GoRemote/sdk/credential"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// sampleSnapshot returns a non-trivial snapshot: two folders, two
// connections, one template, and a workspace with one tab.
func sampleSnapshot(t *testing.T) *Snapshot {
	t.Helper()
	tree := domain.NewTree()
	rootF := &domain.FolderNode{
		ID: domain.NewID(), ParentID: domain.NilID, Name: "Prod",
		Description: "production",
		Defaults:    domain.FolderDefaults{Username: "root", Port: 22},
		Tags:        []string{"prod"}, Color: "#ff0000",
	}
	if err := tree.AddFolder(rootF); err != nil {
		t.Fatal(err)
	}
	childF := &domain.FolderNode{
		ID: domain.NewID(), ParentID: rootF.ID, Name: "EU",
	}
	if err := tree.AddFolder(childF); err != nil {
		t.Fatal(err)
	}
	connA := &domain.ConnectionNode{
		ID: domain.NewID(), ParentID: childF.ID, Name: "web-01",
		ProtocolID: "io.goremote.protocol.ssh", Host: "web-01.eu",
		Port: 22, Username: "ops", AuthMethod: protocol.AuthPassword,
		CredentialRef: credential.Reference{ProviderID: "p", EntryID: "e"},
		Tags:          []string{"web"},
	}
	if err := tree.AddConnection(connA); err != nil {
		t.Fatal(err)
	}
	connB := &domain.ConnectionNode{
		ID: domain.NewID(), ParentID: rootF.ID, Name: "db-01",
		ProtocolID: "io.goremote.protocol.ssh", Host: "db-01",
		Port: 22, Username: "dba",
	}
	if err := tree.AddConnection(connB); err != nil {
		t.Fatal(err)
	}

	tpl := domain.ConnectionTemplate{
		ID: domain.NewID(), Name: "SSH default",
		ProtocolID: "io.goremote.protocol.ssh", Port: 22,
	}
	ws := domain.WorkspaceLayout{
		Bounds:     domain.WindowBounds{X: 10, Y: 20, Width: 1200, Height: 800},
		OpenTabs:   []domain.OpenTab{{ConnectionID: connA.ID, Label: "web-01"}},
		FocusedTab: connA.ID,
	}
	return &Snapshot{
		Tree:      tree,
		Templates: []domain.ConnectionTemplate{tpl},
		Workspace: ws,
		Meta:      Meta{AppVersion: "test-1"},
	}
}

func TestStore_SaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	snap := sampleSnapshot(t)

	if err := s.Save(context.Background(), snap); err != nil {
		t.Fatalf("save: %v", err)
	}
	for _, name := range []string{FileMeta, FileInventory, FileTemplates, FileWorkspace} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing file %s: %v", name, err)
		}
	}

	loaded, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Meta.Version != CurrentVersion {
		t.Errorf("version: got %d want %d", loaded.Meta.Version, CurrentVersion)
	}
	if loaded.Meta.AppVersion != "test-1" {
		t.Errorf("app version: %q", loaded.Meta.AppVersion)
	}

	// Compare tree contents via walk.
	wantFolders, wantConns := flattenSnapshotTree(snap.Tree)
	gotFolders, gotConns := flattenSnapshotTree(loaded.Tree)
	if len(wantFolders) != len(gotFolders) || len(wantConns) != len(gotConns) {
		t.Fatalf("mismatched counts: folders %d/%d conns %d/%d",
			len(gotFolders), len(wantFolders), len(gotConns), len(wantConns))
	}
	for i, f := range wantFolders {
		g := gotFolders[i]
		if f.ID != g.ID || f.Name != g.Name || f.ParentID != g.ParentID {
			t.Errorf("folder[%d] mismatch: %+v vs %+v", i, g, f)
		}
	}
	for i, c := range wantConns {
		g := gotConns[i]
		if c.ID != g.ID || c.Name != g.Name || c.Host != g.Host || c.ParentID != g.ParentID {
			t.Errorf("conn[%d] mismatch: %+v vs %+v", i, g, c)
		}
		if !reflect.DeepEqual(c.CredentialRef, g.CredentialRef) {
			t.Errorf("conn[%d] credref mismatch", i)
		}
	}
	if !reflect.DeepEqual(snap.Templates, loaded.Templates) {
		t.Errorf("templates mismatch:\n got %#v\n want %#v", loaded.Templates, snap.Templates)
	}
	if !reflect.DeepEqual(snap.Workspace, loaded.Workspace) {
		t.Errorf("workspace mismatch:\n got %#v\n want %#v", loaded.Workspace, snap.Workspace)
	}
}

func TestStore_LoadEmptyDir(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	snap, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if snap.Tree == nil {
		t.Fatal("tree nil")
	}
	if snap.Meta.Version != CurrentVersion {
		t.Errorf("fresh meta version = %d", snap.Meta.Version)
	}
}

func TestWriteAtomic_NoPartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.json")
	// Pre-existing content should remain if a write fails.
	if err := WriteAtomic(path, []byte("v1")); err != nil {
		t.Fatalf("write v1: %v", err)
	}

	// Force a deterministic cross-platform failure: create a directory at the
	// target path so replacing it with a regular file fails.
	blocker := filepath.Join(dir, "blocker")
	if err := os.Mkdir(blocker, 0o700); err != nil {
		t.Fatalf("mkdir blocker: %v", err)
	}
	err := WriteAtomic(blocker, []byte("boom"))
	if err == nil {
		t.Fatalf("expected failure writing over directory")
	}
	// No temp file should be left behind in dir.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
	// Original file untouched.
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "v1" {
		t.Errorf("original file mutated: %q err=%v", b, err)
	}
}

func TestWriteAtomic_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := WriteAtomic(path, []byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("b")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "b" {
		t.Errorf("got %q want b", got)
	}
	// No leftover dot-tmp files.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("leftover temp: %s", e.Name())
		}
	}
}

func TestMigrator_RenamesFieldAndRollsBack(t *testing.T) {
	// Register a 0→1 migration that renames a field "old" to "new" inside
	// inventory, plus an optional 0→1 that errors to exercise rollback.
	rename := Migration{From: 0, To: 1, Migrate: func(raw map[string]any) (map[string]any, error) {
		inv, _ := raw["inventory"].(map[string]any)
		if inv != nil {
			if v, ok := inv["old"]; ok {
				inv["new"] = v
				delete(inv, "old")
			}
		}
		return raw, nil
	}}
	mig := &Migrator{Migrations: []Migration{rename}}

	files := map[string][]byte{
		FileInventory: []byte(`{"folders":[],"connections":[],"old":"value"}`),
	}
	meta := &Meta{Version: 0}
	out, err := mig.Run(meta, files)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if meta.Version != CurrentVersion {
		t.Errorf("version not bumped: %d", meta.Version)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out[FileInventory], &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["new"] != "value" {
		t.Errorf("rename didn't happen: %v", decoded)
	}
	if _, ok := decoded["old"]; ok {
		t.Errorf("old field still present: %v", decoded)
	}

	// Rollback: a failing migration must not mutate meta or files.
	failMig := &Migrator{Migrations: []Migration{
		{From: 0, To: 1, Migrate: func(raw map[string]any) (map[string]any, error) {
			return nil, errors.New("boom")
		}},
	}}
	originalMeta := Meta{Version: 0}
	metaCopy := originalMeta
	originalFiles := map[string][]byte{FileInventory: []byte(`{"a":1}`)}
	out2, err := failMig.Run(&metaCopy, originalFiles)
	if err == nil {
		t.Fatal("expected error")
	}
	if metaCopy.Version != 0 {
		t.Errorf("meta mutated on rollback: %d", metaCopy.Version)
	}
	if !reflect.DeepEqual(out2, originalFiles) {
		// The returned files should be byte-equal to the originals.
		if string(out2[FileInventory]) != string(originalFiles[FileInventory]) {
			t.Errorf("files mutated on rollback: %s vs %s",
				out2[FileInventory], originalFiles[FileInventory])
		}
	}
}

func TestMigrator_AlreadyAtCurrent(t *testing.T) {
	mig := DefaultMigrator()
	meta := &Meta{Version: CurrentVersion}
	files := map[string][]byte{FileMeta: []byte(`{"version":1}`)}
	out, err := mig.Run(meta, files)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, files) {
		t.Errorf("files changed unnecessarily")
	}
}

func TestRestoreRejectsOversizedArchive(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := s.Save(context.Background(), sampleSnapshot(t)); err != nil {
		t.Fatalf("save: %v", err)
	}

	oldEntries, oldBytes, oldFileBytes := maxRestoreEntries, maxRestoreBytes, maxRestoreFileBytes
	maxRestoreEntries, maxRestoreBytes, maxRestoreFileBytes = 8, 64, 32
	defer func() {
		maxRestoreEntries, maxRestoreBytes, maxRestoreFileBytes = oldEntries, oldBytes, oldFileBytes
	}()

	bad := filepath.Join(dir, "oversized.zip")
	f, err := os.OpenFile(bad, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("inventory.json")
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("x"), 128)); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	if err := s.Restore(context.Background(), bad); err == nil {
		t.Fatal("expected restore to reject oversized archive")
	}
}

func TestCheckedCopyLimitsRejectsOverflow(t *testing.T) {
	maxInt64 := int64(^uint64(0) >> 1)
	_, _, err := checkedCopyLimits(uint64(maxInt64))
	if err == nil {
		t.Fatal("expected oversized limit to fail")
	}
}

func TestMigrator_NewerThanSupported(t *testing.T) {
	mig := DefaultMigrator()
	meta := &Meta{Version: CurrentVersion + 1}
	_, err := mig.Run(meta, nil)
	if err == nil {
		t.Fatal("expected error for future version")
	}
}

func TestValidate_MissingParent(t *testing.T) {
	// Build via RawSnapshot because domain.Tree refuses inconsistent data.
	missingParent := domain.NewID()
	raw := RawSnapshot{
		Folders: []domain.FolderNode{
			{ID: domain.NewID(), ParentID: missingParent, Name: "orphan"},
		},
	}
	issues := ValidateRawSnapshot(raw)
	if len(issues) == 0 {
		t.Fatal("expected issue")
	}
	found := false
	for _, i := range issues {
		if i.Code == CodeMissingFolderParent && i.Severity == SeverityError {
			found = true
		}
	}
	if !found {
		t.Errorf("no missing_folder_parent issue: %+v", issues)
	}
}

func TestValidate_Cycle(t *testing.T) {
	a := domain.NewID()
	b := domain.NewID()
	raw := RawSnapshot{
		Folders: []domain.FolderNode{
			{ID: a, ParentID: b, Name: "A"},
			{ID: b, ParentID: a, Name: "B"},
		},
	}
	issues := ValidateRawSnapshot(raw)
	var cycles int
	for _, i := range issues {
		if i.Code == CodeFolderCycle {
			cycles++
		}
	}
	if cycles != 1 {
		t.Errorf("expected exactly 1 cycle issue (deduped), got %d: %+v", cycles, issues)
	}
}

func TestValidate_OrphanWorkspaceTab(t *testing.T) {
	snap := sampleSnapshot(t)
	snap.Workspace.OpenTabs = append(snap.Workspace.OpenTabs, domain.OpenTab{
		ConnectionID: domain.NewID(), Label: "ghost",
	})
	issues := Validate(snap)
	var found bool
	for _, i := range issues {
		if i.Code == CodeOrphanWorkspaceTab {
			if i.Severity != SeverityWarn {
				t.Errorf("orphan tab should be warn, got %s", i.Severity)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("no orphan_workspace_tab issue: %+v", issues)
	}
}

func TestValidate_Clean(t *testing.T) {
	snap := sampleSnapshot(t)
	if issues := Validate(snap); len(issues) != 0 {
		t.Errorf("expected no issues, got %+v", issues)
	}
}

func TestValidate_DuplicateID(t *testing.T) {
	dupID := domain.NewID()
	raw := RawSnapshot{
		Folders:     []domain.FolderNode{{ID: dupID, ParentID: domain.NilID, Name: "F"}},
		Connections: []domain.ConnectionNode{{ID: dupID, ParentID: domain.NilID, Name: "C"}},
	}
	issues := ValidateRawSnapshot(raw)
	var found bool
	for _, i := range issues {
		if i.Code == CodeDuplicateID {
			found = true
		}
	}
	if !found {
		t.Errorf("no duplicate_id issue: %+v", issues)
	}
}

func TestBackupRestore(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	snap := sampleSnapshot(t)
	if err := s.Save(context.Background(), snap); err != nil {
		t.Fatal(err)
	}

	zipPath, err := s.Backup(context.Background())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if !strings.HasSuffix(zipPath, ".zip") {
		t.Errorf("unexpected path %s", zipPath)
	}
	// Verify archive contains inventory.json and no backups/ entries.
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	zr.Close()
	mustContain := func(n string) {
		for _, x := range names {
			if x == n {
				return
			}
		}
		t.Errorf("archive missing %s; got %v", n, names)
	}
	mustContain(FileInventory)
	mustContain(FileMeta)
	for _, x := range names {
		if strings.HasPrefix(x, BackupsDirName+"/") || x == BackupsDirName {
			t.Errorf("archive should exclude backups/: %s", x)
		}
	}

	// Mutate store state, then restore.
	mutated := sampleSnapshot(t)
	mutated.Templates[0].Name = "MUTATED"
	if err := s.Save(context.Background(), mutated); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(context.Background(), zipPath); err != nil {
		t.Fatalf("restore: %v", err)
	}
	reloaded, err := s.Load(context.Background())
	if err != nil {
		t.Fatalf("post-restore load: %v", err)
	}
	if reloaded.Templates[0].Name == "MUTATED" {
		t.Errorf("restore did not revert templates")
	}
	// Restore should have taken a safety backup, so backups dir has ≥2 zips.
	entries, _ := os.ReadDir(filepath.Join(dir, BackupsDirName))
	var zips int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".zip") {
			zips++
		}
	}
	if zips < 2 {
		t.Errorf("expected ≥2 backups after restore, got %d", zips)
	}
}

func TestBackupRetention(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	// Use a time function we can advance so each backup gets a unique name.
	clock := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	if err := s.Save(context.Background(), sampleSnapshot(t)); err != nil {
		t.Fatal(err)
	}
	// Create DefaultBackupRetention+3 backups.
	for i := 0; i < DefaultBackupRetention+3; i++ {
		if _, err := s.Backup(context.Background()); err != nil {
			t.Fatalf("backup %d: %v", i, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, BackupsDirName))
	if err != nil {
		t.Fatal(err)
	}
	var zips []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".zip") {
			zips = append(zips, e.Name())
		}
	}
	if len(zips) != DefaultBackupRetention {
		t.Errorf("retention: have %d zips, want %d: %v", len(zips), DefaultBackupRetention, zips)
	}
	sort.Strings(zips)
	// The earliest should have been pruned; earliest surviving timestamp
	// should be >= first-4 (3 excess + 1).
	first := zips[0]
	if !strings.HasPrefix(first, "20240101T000") {
		t.Errorf("unexpected earliest remaining: %s", first)
	}
}

func TestStore_ConcurrentLoadSave(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := s.Save(context.Background(), sampleSnapshot(t)); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Many readers.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				snap, err := s.Load(context.Background())
				if err != nil {
					t.Errorf("load: %v", err)
					return
				}
				if snap.Tree == nil {
					t.Errorf("nil tree")
					return
				}
			}
		}()
	}
	// One writer.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			snap := sampleSnapshot(t)
			if err := s.Save(context.Background(), snap); err != nil {
				t.Errorf("save: %v", err)
				return
			}
		}
	}()

	// Let writer run for a bit, then signal readers to stop.
	time.Sleep(50 * time.Millisecond)
	// Wait for writer to finish first.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	// Writer finishes eventually; then stop readers.
	// We approximate by polling: stop after 500ms max.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	close(stop)
	<-done
}

func TestRestore_RejectsUnsafePath(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := s.Save(context.Background(), sampleSnapshot(t)); err != nil {
		t.Fatal(err)
	}
	// Craft a zip with a traversal entry.
	bad := filepath.Join(dir, "bad.zip")
	f, err := os.Create(bad)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../escape.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("{}"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if err := s.Restore(context.Background(), bad); err == nil {
		t.Error("expected rejection of traversal entry")
	}
}
