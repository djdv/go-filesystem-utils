package daemon

import (
	"context"
	"os"
	"os/signal"
	"time"

	cmdsenv "github.com/djdv/go-filesystem-utils/internal/cmds/environment"
	"github.com/djdv/go-filesystem-utils/internal/cmds/environment/stop"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// setupStopperAPI primes the stopper interface
// and emits it's name to the client.
func setupStopperAPI(ctx context.Context, apiPath []string,
	serviceEnv cmdsenv.Environment,
) (<-chan *Response, <-chan stop.Reason, error) {
	var (
		stopper          = serviceEnv.Stopper()
		stopReasons, err = stopper.Initialize(ctx)
		responses        = make(chan *Response, 1)
	)
	defer close(responses)
	responses <- stopListenerResponse(apiPath)
	return responses, stopReasons, err
}

func stopOnSignal(ctx context.Context,
	stopper stop.Stopper, notifySignal os.Signal,
) (responses, errCh) {
	var (
		errs      = make(chan error, 1)
		responses = make(chan *Response, 1)
		sigChan   = make(chan os.Signal, 1)
	)
	defer close(responses)
	signal.Notify(sigChan, notifySignal)
	responses <- signalListenerResponse(notifySignal)
	go func() {
		defer close(sigChan)
		defer close(errs)
		defer signal.Reset(notifySignal)
		select {
		case <-sigChan:
			const reason = stop.Canceled
			if sErr := stopper.Stop(reason); sErr != nil {
				errs <- sErr
			}
		case <-ctx.Done():
		}
	}()
	return responses, errs
}

func stopOnRequestCancel(ctx context.Context, stopper stop.Stopper, request *cmds.Request) (responses, errCh) {
	var (
		triggerCtx = request.Context
		errs       = make(chan error)
		responses  = make(chan *Response, 1)
	)
	defer close(responses)
	responses <- cmdsRequestListenerResponse()
	go func() {
		defer close(errs)
		select {
		case <-triggerCtx.Done():
			const reason = stop.Canceled
			if sErr := stopper.Stop(reason); sErr != nil {
				select {
				case errs <- sErr:
				case <-ctx.Done():
				}
			}
		case <-ctx.Done():
		}
	}()
	return responses, errs
}

func stopOnIdleEvent(ctx context.Context,
	serviceEnv cmdsenv.Environment, interval time.Duration,
) (responses, errCh) {
	var (
		// NOTE [placeholder]: This build is never busy.
		// The ipc env should be used to query activity when implemented.
		daemon      = serviceEnv.Daemon()
		mounter     = daemon.Mounter()
		checkIfBusy = func() (bool, error) {
			instances, err := mounter.List(ctx)
			if err != nil {
				return false, err
			}

			var activeInstances bool
			for range instances {
				activeInstances = true
			}
			return activeInstances, nil
		}
		responses = make(chan *Response, 1)
	)
	defer close(responses)
	responses <- tickerListenerResponse(interval, "is-service-idle-every")

	stopper := serviceEnv.Stopper()
	return responses, stopOnIdle(ctx, stopper, interval, checkIfBusy)
}

type isBusyFunc func() (bool, error)

func stopOnIdle(ctx context.Context, stopper stop.Stopper,
	checkInterval time.Duration, checkIfBusy isBusyFunc,
) <-chan error {
	errs := make(chan error)
	go func() {
		idleCheckTicker := time.NewTicker(checkInterval)
		defer idleCheckTicker.Stop()
		defer close(errs)
		for {
			select {
			case <-idleCheckTicker.C:
				busy, err := checkIfBusy()
				if err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
					}
					return
				}
				if busy {
					continue
				}
				if err := stopper.Stop(stop.Idle); err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
					}
				}
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return errs
}
