//go:build unix

package client

import (
	"os"
	"os/signal"
	"syscall"
)

func procRunning(proc *os.Process) bool {
	// SUSv4;BSi7 - kill
	// If sig is 0, error checking is performed but no signal is actually sent.
	return proc.Signal(syscall.Signal(0)) != nil
}

func childProcInit() {
	// See: [os/signal] documentation.
	// If our parent process doesn't follow
	// protocol, writes to stdio will get us
	// killed by the OS. Ignoring this signal
	// will let use receive a less harmful
	// `EPIPE` error value.
	signal.Ignore(syscall.SIGPIPE)
}

func emancipatedSubproc() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true,
	}
}
