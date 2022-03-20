package daemon

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/djdv/go-filesystem-utils/cmd/environment"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// setupStopperAPI primes the stopper interface
// and emits it's name to the client.
func setupStopperAPI(ctx context.Context, apiPath []string,
	daemon environment.Daemon) (<-chan *Response, <-chan environment.Reason, error) {
	var (
		stopper          = daemon.Stopper()
		stopReasons, err = stopper.Initialize(ctx)
		responses        = make(chan *Response, 1)
	)
	defer close(responses)
	responses <- stopListenerResponse(apiPath)
	return responses, stopReasons, err
}

func stopOnSignal(ctx context.Context,
	stopper environment.Stopper, notifySignal os.Signal) (responses, errCh) {
	var (
		stop      = stopper.Stop
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
			const reason = environment.Canceled
			if sErr := stop(reason); sErr != nil {
				errs <- sErr
			}
		case <-ctx.Done():
		}
	}()
	return responses, errs
}

func stopOnRequestCancel(ctx context.Context, stopper environment.Stopper, request *cmds.Request) (responses, errCh) {
	var (
		triggerCtx = request.Context
		stop       = stopper.Stop
		errs       = make(chan error)
		responses  = make(chan *Response, 1)
	)
	defer close(responses)
	responses <- cmdsRequestListenerResponse()
	go func() {
		defer close(errs)
		select {
		case <-triggerCtx.Done():
			const reason = environment.Canceled
			if sErr := stop(reason); sErr != nil {
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
	serviceEnv environment.Environment, interval time.Duration) (responses, errCh) {
	var (
		// NOTE [placeholder]: This build is never busy.
		// The ipc env should be used to query activity when implemented.
		daemon      = serviceEnv.Daemon()
		checkIfBusy = func() (bool, error) {
			instances, err := daemon.List()
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

	stopper := daemon.Stopper()
	return responses, stopOnIdle(ctx, stopper, interval, checkIfBusy)
}

type isBusyFunc func() (bool, error)

func stopOnIdle(ctx context.Context, stopper environment.Stopper,
	checkInterval time.Duration, checkIfBusy isBusyFunc) <-chan error {
	errs := make(chan error, 1)
	go func() {
		idleCheckTicker := time.NewTicker(checkInterval)
		defer idleCheckTicker.Stop()
		defer close(errs)
		for {
			select {
			case <-idleCheckTicker.C:
				busy, err := checkIfBusy()
				if err != nil {
					errs <- err
					return
				}
				if busy {
					continue
				}
				if err := stopper.Stop(environment.Idle); err != nil {
					errs <- err
				}
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return errs
}
