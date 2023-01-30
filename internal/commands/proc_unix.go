//go:build unix

package commands

import (
	"os"
	"syscall"
)

func procRunning(proc *os.Process) bool {
	// SUSv4;BSi7 - kill
	// If sig is 0, error checking is performed but no signal is actually sent.
	return proc.Signal(syscall.Signal(0)) != nil
}
