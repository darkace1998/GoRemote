package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// dumpCrashIfPanicking is the deferred top-of-main recover. When it
// catches a panic and crash reporting is enabled, it writes a crash log
// containing the version, panic value, and full goroutine stack to
// <state>/crashes/crash-<RFC3339-utc>.log, then re-panics so the
// process still exits non-zero (important for OS crash dialogs and CI).
//
// Called via `defer dumpCrashIfPanicking()` at the start of main(). Must
// not depend on the slog default handler — by the time it runs, fileSink
// may already be closed.
func dumpCrashIfPanicking() {
	r := recover()
	if r == nil {
		return
	}
	if !crashState.enabled {
		// Re-panic untouched if user opted out.
		panic(r)
	}
	dir := crashState.dir
	if dir == "" {
		dir = os.TempDir()
	}
	crashDir := filepath.Join(dir, "crashes")
	if err := os.MkdirAll(crashDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "crash dump: mkdir: %v\n", err)
		panic(r)
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	path := filepath.Join(crashDir, "crash-"+stamp+".log")
	// #nosec G304 -- crash log path is derived from the configured crash directory and a fixed filename pattern.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crash dump: open: %v\n", err)
		panic(r)
	}
	fmt.Fprintf(f, "goremote crash report\n")
	fmt.Fprintf(f, "version: %s\n", Version)
	fmt.Fprintf(f, "time: %s\n", time.Now().UTC().Format(time.RFC3339Nano))
	fmt.Fprintf(f, "panic: %v\n\n", r)
	_, _ = f.Write(debug.Stack())
	_ = f.Close()
	fmt.Fprintf(os.Stderr, "goremote crashed; report written to %s\n", path)
	panic(r)
}
