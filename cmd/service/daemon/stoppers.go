package daemon

import (
	"context"
	"os"
	"os/signal"
	"time"

	serviceenv "github.com/djdv/go-filesystem-utils/cmd/environment/service"
	stopenv "github.com/djdv/go-filesystem-utils/cmd/environment/service/daemon/stop"
)

// setupStopper emits starting, and primes the stopper interface.
func setupStopper(ctx context.Context,
	serviceEnv serviceenv.Environment) (stopenv.Environment,
	<-chan stopenv.Reason, error) {
	stopper := serviceEnv.Daemon().Stopper()
	stopReasons, err := stopper.Initialize(ctx)
	if err != nil {
		return nil, nil, err
	}

	return stopper, stopReasons, nil
}

func stopOnSignal(ctx context.Context, stopper stopenv.Environment,
	stopReason stopenv.Reason, notifySignal os.Signal) <-chan error {
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
				select {
				case errs <- err:
				case <-ctx.Done():
				}
			}
		case <-ctx.Done():
		}
	}()

	return errs
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
