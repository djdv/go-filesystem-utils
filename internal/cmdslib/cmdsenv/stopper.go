package cmdsenv

import (
	"context"
	"fmt"
)

type (
	Reason  uint
	Stopper interface {
		Initialize(context.Context) (<-chan Reason, error)
		Stop(Reason) error
	}
	stopper struct {
		stopCtx  context.Context
		stopChan chan Reason
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

func (env *stopper) Initialize(ctx context.Context) (<-chan Reason, error) {
	if env.stopChan != nil {
		return nil, fmt.Errorf("stopper already initialized")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stopChan := make(chan Reason)
	env.stopChan = stopChan
	env.stopCtx = ctx
	return stopChan, nil
}

func (env *stopper) Stop(reason Reason) error {
	var (
		ctx      = env.stopCtx
		stopChan = env.stopChan
	)
	if stopChan == nil {
		return fmt.Errorf("stopper not initialized")
	}
	env.stopChan = nil

	defer close(stopChan)
	select {
	case stopChan <- reason:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
