package cmdsenv

import (
	"context"
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/cmds/environment/stop"
)

type stopper struct {
	stopCtx  context.Context
	stopChan chan stop.Reason
}

func (env *stopper) Initialize(ctx context.Context) (<-chan stop.Reason, error) {
	if env.stopChan != nil {
		return nil, fmt.Errorf("stopper already initialized")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stopChan := make(chan stop.Reason)
	env.stopChan = stopChan
	env.stopCtx = ctx
	return stopChan, nil
}

func (env *stopper) Stop(reason stop.Reason) error {
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
