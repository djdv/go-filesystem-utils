package daemon

import (
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/generic"
)

type ShutdownDisposition uint8

const (
	dontShutdown ShutdownDisposition = iota
	// ShutdownPatient will stop accepting new connections
	// and wait for all clients to either disconnect or become idle,
	// before stopping services.
	ShutdownPatient
	// ShutdownShort will stop accepting new connections
	// and forcibly disconnect clients after some time,
	// before stopping services.
	ShutdownShort
	// ShutdownShort will stop accepting new connections
	// and forcibly disconnect clients immediately,
	// before stopping services.
	ShutdownImmediate
	minimumShutdown = ShutdownPatient
	maximumShutdown = ShutdownImmediate
)

func (level ShutdownDisposition) String() string {
	switch level {
	case ShutdownPatient:
		return "patient"
	case ShutdownShort:
		return "short"
	case ShutdownImmediate:
		return "immediate"
	default:
		return fmt.Sprintf("invalid: %d", level)
	}
}

func ParseShutdownLevel(level string) (ShutdownDisposition, error) {
	return generic.ParseEnum(minimumShutdown, maximumShutdown, level)
}
