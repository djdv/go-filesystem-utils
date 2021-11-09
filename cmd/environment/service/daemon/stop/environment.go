package stop

import (
	"context"
	"fmt"
)

type (
	Reason      uint
	Environment interface {
		Initialize(context.Context) (<-chan Reason, error)
		Stop(Reason) error
	}
	environment struct {
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

func MakeEnvironment() Environment { return &environment{} }

func (env *environment) Initialize(ctx context.Context) (<-chan Reason, error) {
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

func (env *environment) Stop(reason Reason) error {
	var (
		ctx      = env.stopCtx
		stopChan = env.stopChan
	)
	if stopChan == nil {
		return fmt.Errorf("stopper not initialized")
	}

	defer close(stopChan)
	select {
	case stopChan <- reason:
		env.stopChan = nil
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
