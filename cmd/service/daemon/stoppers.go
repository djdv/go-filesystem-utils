package daemon

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/djdv/go-filesystem-utils/cmd/environment"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// setupStopper primes the stopper interface
// and emits it's name to the client.
func setupStopper(ctx context.Context,
	request *cmds.Request, runEnv *runEnv) (<-chan environment.Reason, error) {
	stopper, stopReasons, err := makeStopper(ctx, runEnv.Environment)
	if err != nil {
		return nil, err
	}
	runEnv.stopper = stopper

	stopperAPIPath := append(request.Path, stop.Name)
	if err := runEnv.Emit(stopListenerResponse(stopperAPIPath...)); err != nil {
		return nil, err
	}

	return stopReasons, nil
}

func makeStopper(ctx context.Context,
	serviceEnv environment.Environment) (environment.Stopper,
	<-chan environment.Reason, error) {
	stopper := serviceEnv.Daemon().Stopper()
	stopReasons, err := stopper.Initialize(ctx)
	if err != nil {
		return nil, nil, err
	}

	return stopper, stopReasons, nil
}

func stopOnSignal(ctx context.Context,
	stopper environment.Stopper, stopReason environment.Reason,
	notifySignal os.Signal) <-chan error {
	var (
		errs    = make(chan error)
		sigChan = make(chan os.Signal, 1)
	)
	signal.Notify(sigChan, notifySignal)
	go func() {
		defer close(sigChan)
		defer close(errs)
		defer signal.Reset(notifySignal)
		select {
		case <-sigChan:
			if err := stopper.Stop(stopReason); err != nil {
				errs <- err
			}
		case <-ctx.Done():
		}
	}()
	return errs
}

func stopOnRequestCancel(ctx context.Context, request *cmds.Request,
	stopper environment.Stopper, stopReason environment.Reason) <-chan error {
	var (
		triggerCtx = request.Context
		stop       = stopper.Stop
		errs       = make(chan error, 1)
	)
	go func() {
		defer close(errs)
		select {
		case <-triggerCtx.Done():
			if sErr := stop(stopReason); sErr != nil {
				errs <- sErr
			}
		case <-ctx.Done():
		}
	}()
	return errs
}

func stopOnIdleEvent(ctx context.Context,
	runEnv *runEnv, interval time.Duration) taskErr {
	// NOTE [placeholder]: This build is never busy.
	// The ipc env should be used to query activity when implemented.
	checkIfBusy := func() (bool, error) {
		return false, nil
	}
	if err := runEnv.Emit(tickerListenerResponse(interval, "is-service-idle-every")); err != nil {
		return taskErr{foreground: err}
	}
	return taskErr{
		background: stopOnIdle(ctx, runEnv.stopper, interval, checkIfBusy),
	}
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
