// Package sync implements optional external storage backends for the
// goremote workspace directory.
//
// The first (and currently only) backend is a thin wrapper around the
// system `git` CLI that, when enabled, treats the workspace directory as
// a git repository and runs add/commit/push after every successful Save.
//
// The CLI is invoked rather than a Go-native library to keep dependency
// surface zero; the user is expected to have git installed if they
// enable this feature. All commands run with a short context timeout so
// a hung remote cannot block the UI thread, and failures are surfaced
// as wrapped errors that the caller logs at warn level (sync is
// best-effort, never fatal).
package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Default timeout per git invocation. Long enough for a slow push over
// a high-latency link, short enough that a wedged remote eventually
// gives the UI back its save loop.
const defaultTimeout = 30 * time.Second

// GitSync wraps a workspace directory that should be mirrored as a git
// repository. The zero value is unusable — use New.
type GitSync struct {
	dir     string
	remote  string
	branch  string
	gitPath string
}

// Config configures a GitSync. Remote may be empty (commits stay local).
// Branch may be empty (defaults to "main").
type Config struct {
	Dir    string
	Remote string
	Branch string
}

// New constructs a GitSync. It returns an error only when Config.Dir
// is empty; the actual `git` binary is resolved lazily on first use so
// callers can construct the value without git being installed.
func New(c Config) (*GitSync, error) {
	if c.Dir == "" {
		return nil, errors.New("sync: dir is required")
	}
	abs, err := filepath.Abs(c.Dir)
	if err != nil {
		return nil, fmt.Errorf("sync: abs %s: %w", c.Dir, err)
	}
	branch := strings.TrimSpace(c.Branch)
	if branch == "" {
		branch = "main"
	}
	return &GitSync{dir: abs, remote: strings.TrimSpace(c.Remote), branch: branch}, nil
}

// Available reports whether the git CLI is reachable on PATH.
func (g *GitSync) Available() bool {
	_, err := g.locate()
	return err == nil
}

// Init makes sure the workspace directory is a git repository on the
// configured branch and (if Remote is set) has the remote registered.
// It is idempotent.
func (g *GitSync) Init(ctx context.Context) error {
	if _, err := g.locate(); err != nil {
		return err
	}
	if !g.isRepo(ctx) {
		if _, err := g.run(ctx, "init", "-b", g.branch); err != nil {
			return err
		}
		_, _ = g.run(ctx, "config", "user.name", "goremote")
		_, _ = g.run(ctx, "config", "user.email", "goremote@localhost")
	}
	if g.remote != "" {
		out, err := g.run(ctx, "remote")
		if err != nil {
			return err
		}
		if !lineContains(out, "origin") {
			if _, err := g.run(ctx, "remote", "add", "origin", g.remote); err != nil {
				return err
			}
		} else {
			_, _ = g.run(ctx, "remote", "set-url", "origin", g.remote)
		}
	}
	return nil
}

// CommitAndPush stages every change, commits with the supplied message,
// and pushes to the configured remote. Returns nil when there is
// nothing to commit (empty diff).
func (g *GitSync) CommitAndPush(ctx context.Context, msg string) error {
	if _, err := g.locate(); err != nil {
		return err
	}
	if err := g.Init(ctx); err != nil {
		return err
	}
	if _, err := g.run(ctx, "add", "-A"); err != nil {
		return err
	}
	st, err := g.run(ctx, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(st) == "" {
		return nil
	}
	if msg == "" {
		msg = "goremote: workspace update"
	}
	if _, err := g.run(ctx, "commit", "-m", msg); err != nil {
		return err
	}
	if g.remote == "" {
		return nil
	}
	if _, err := g.run(ctx, "rev-parse", "--abbrev-ref", g.branch+"@{upstream}"); err != nil {
		_, perr := g.run(ctx, "push", "--set-upstream", "origin", g.branch)
		return perr
	}
	_, perr := g.run(ctx, "push")
	return perr
}

func (g *GitSync) isRepo(ctx context.Context) bool {
	_, err := g.run(ctx, "rev-parse", "--git-dir")
	return err == nil
}

func (g *GitSync) locate() (string, error) {
	if g.gitPath != "" {
		return g.gitPath, nil
	}
	p, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("sync: git binary not found on PATH: %w", err)
	}
	g.gitPath = p
	return p, nil
}

func (g *GitSync) run(ctx context.Context, args ...string) (string, error) {
	gitPath, err := g.locate()
	if err != nil {
		return "", err
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, gitPath, args...)
	cmd.Dir = g.dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

func lineContains(out, needle string) bool {
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) == needle {
			return true
		}
	}
	return false
}
