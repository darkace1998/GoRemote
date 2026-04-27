//go:build !windows

package extlaunch

import (
	"os"
)

// interruptSignal is the signal used to politely ask a child process to exit.
var interruptSignal os.Signal = os.Interrupt
