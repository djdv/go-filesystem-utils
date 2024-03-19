//go:build unix

package daemon

import (
	"os/signal"
	"syscall"
)

func childProcInit() {
	// See: [os/signal] documentation.
	// If our parent process doesn't follow
	// protocol, writes to stdio will get us
	// killed by the OS. Ignoring this signal
	// will let use receive a less harmful
	// `EPIPE` error value.
	signal.Ignore(syscall.SIGPIPE)
}
