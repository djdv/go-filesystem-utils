package daemon

import (
	"context"
	"os"
	"os/signal"
	"time"

	serviceenv "github.com/djdv/go-filesystem-utils/cmd/environment/service"
	stopenv "github.com/djdv/go-filesystem-utils/cmd/environment/service/daemon/stop"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// setupStopper primes the stopper interface
// and emits it's name to the client.
func setupStopper(ctx context.Context,
	request *cmds.Request, emitter cmds.ResponseEmitter,
	serviceEnv serviceenv.Environment) (stopenv.Environment, <-chan stopenv.Reason, error) {
	stopper, stopReasons, err := initStopper(ctx, serviceEnv)
	if err != nil {
		return nil, nil, err
	}
	stopPath := append(request.Path, stop.Name)
	if err := emitStopListener(emitter, stopPath...); err != nil {
		return nil, nil, err
	}
	return stopper, stopReasons, nil
}

func initStopper(ctx context.Context,
	serviceEnv serviceenv.Environment) (stopenv.Environment,
	<-chan stopenv.Reason, error) {
	stopper := serviceEnv.Daemon().Stopper()
	stopReasons, err := stopper.Initialize(ctx)
	if err != nil {
		return nil, nil, err
	}

	return stopper, stopReasons, nil
}

func listenForGoSignals(ctx context.Context,
	emitter cmds.ResponseEmitter, request *cmds.Request,
	stopper stopenv.Environment) (<-chan error, error) {
	osErrs, err := listenForOSSignal(ctx,
		emitter, stopper,
		stopenv.Canceled, os.Interrupt)
	if err != nil {
		return nil, err
	}
	cmdsErrs, err := listenForRequestCancel(ctx,
		emitter, request,
		stopper, stopenv.Canceled)
	if err != nil {
		return nil, err
	}
	return joinErrs(osErrs, cmdsErrs), nil
}

func listenForOSSignal(ctx context.Context,
	emitter cmds.ResponseEmitter, stopper stopenv.Environment,
	stopReason stopenv.Reason, notifySignal os.Signal) (<-chan error, error) {
	stopErrs := stopOnSignal(ctx, stopper, stopReason, notifySignal)
	if err := emitSignalListener(emitter, notifySignal); err != nil {
		return nil, err
	}
	return stopErrs, nil
}

func stopOnSignal(ctx context.Context,
	stopper stopenv.Environment, stopReason stopenv.Reason,
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

// TODO: review; jank?
func listenForRequestCancel(ctx context.Context,
	emitter cmds.ResponseEmitter, request *cmds.Request,
	stopper stopenv.Environment, stopReason stopenv.Reason) (<-chan error, error) {
	if err := emitCmdsListener(emitter); err != nil {
		return nil, err
	}
	var (
		triggerCtx = request.Context
		stop       = stopper.Stop
		errs       = make(chan error, 1)
	)
	go func() {
		defer close(errs)
		select {
		case <-triggerCtx.Done():
			if err := stop(stopReason); err != nil {
				errs <- err
			} else {
				errs <- triggerCtx.Err()
			}
		case <-ctx.Done():
		}
	}()

	return errs, nil
}

func listenForIdleEvent(ctx context.Context, emitter cmds.ResponseEmitter,
	stopper stopenv.Environment, interval time.Duration) (<-chan error, error) {
	// NOTE [placeholder]: This build is never busy.
	// The ipc env should be used to query activity when implemented.
	checkIfBusy := func() (bool, error) {
		return false, nil
	}
	if err := emitTickerListener(emitter,
		interval, "is-service-idle-every"); err != nil {
		return nil, err
	}
	return stopOnIdle(ctx, stopper, interval, checkIfBusy), nil
}

type isBusyFunc func() (bool, error)

// TODO: review; jank?
func stopOnIdle(ctx context.Context, stopper stopenv.Environment,
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
				if err := stopper.Stop(stopenv.Idle); err != nil {
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
