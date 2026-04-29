package onepassword

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
)

// Runner abstracts process invocation so tests can inject a fake without
// shelling out to a real `op` binary.
//
// Implementations must:
//   - feed stdin (which may be empty) to the spawned process,
//   - return the process's stdout and stderr separately,
//   - return the process's exit code (0 for success, the actual code for
//     CLI-reported errors), and
//   - return a non-nil err only when the process could not be spawned at
//     all (e.g. binary not found, context cancelled). A non-zero exit
//     code is NOT an err — callers inspect exitCode.
type Runner interface {
	Run(ctx context.Context, name string, args []string, stdin []byte, env []string) (stdout, stderr []byte, exitCode int, err error)
}

// execRunner is the production Runner; it spawns processes via os/exec.
type execRunner struct{}

// Run implements Runner using os/exec.
func (execRunner) Run(ctx context.Context, name string, args []string, stdin []byte, env []string) ([]byte, []byte, int, error) {
	// #nosec G204 -- name is a resolved CLI path and args are passed directly without a shell.
	cmd := exec.CommandContext(ctx, name, args...)
	if env != nil {
		cmd.Env = env
	}
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	} else {
		cmd.Stdin = io.LimitReader(bytes.NewReader(nil), 0)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return stdout.Bytes(), stderr.Bytes(), ee.ExitCode(), nil
		}
		return stdout.Bytes(), stderr.Bytes(), -1, err
	}
	return stdout.Bytes(), stderr.Bytes(), 0, nil
}
