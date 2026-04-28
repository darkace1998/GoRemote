package sync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNewRequiresDir(t *testing.T) {
	t.Parallel()
	if _, err := New(Config{}); err == nil {
		t.Fatalf("New(empty) = nil err, want error")
	}
}

func TestNewDefaultsBranch(t *testing.T) {
	t.Parallel()
	g, err := New(Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if g.branch != "main" {
		t.Errorf("branch = %q, want main", g.branch)
	}
}

func TestCommitAndPushNoGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	g, err := New(Config{Dir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Empty workspace → init creates repo, then no diff, no commit.
	if err := g.CommitAndPush(context.Background(), "first"); err != nil {
		t.Fatalf("CommitAndPush(empty): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf(".git not created: %v", err)
	}
	// Now write a file and commit again.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := g.CommitAndPush(context.Background(), "add a"); err != nil {
		t.Fatalf("CommitAndPush(content): %v", err)
	}
	// Idempotent: second invocation with no changes is a no-op.
	if err := g.CommitAndPush(context.Background(), "noop"); err != nil {
		t.Fatalf("CommitAndPush(noop): %v", err)
	}
	// Verify exactly one commit was created.
	out, err := exec.Command("git", "-C", dir, "log", "--oneline").Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if got := lineCount(string(out)); got != 1 {
		t.Errorf("commit count = %d, want 1; output:\n%s", got, out)
	}
}

func lineCount(s string) int {
	n := 0
	for _, r := range s {
		if r == '\n' {
			n++
		}
	}
	if len(s) > 0 && s[len(s)-1] != '\n' {
		n++
	}
	return n
}
