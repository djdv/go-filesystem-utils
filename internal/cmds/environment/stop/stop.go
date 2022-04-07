package stop

import (
	"context"
)

type (
	Reason  uint
	Stopper interface {
		Initialize(context.Context) (<-chan Reason, error)
		Stop(Reason) error
	}
)

//go:generate stringer -type=Reason -linecomment
const (
	_         Reason = iota
	Canceled         // request was canceled
	Idle             // service was idle
	Requested        // stop was requested
	Error            // runtime error caused stop to be called
)
