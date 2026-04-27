//go:build windows

package extlaunch

import "os"

// interruptSignal is the signal used to politely ask a child process to exit.
// On Windows, os.Interrupt is largely advisory and is overridden in
// sendInterrupt to call Kill directly; this declaration exists for symmetry.
var interruptSignal os.Signal = os.Interrupt
