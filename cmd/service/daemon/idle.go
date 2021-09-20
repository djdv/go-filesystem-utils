package daemon

import (
	"context"
	"time"

	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment/daemon"
)

func stopIfNotBusy(ctx context.Context,
	checkInterval time.Duration, ipcEnv environment.Environment) <-chan error {
	var (
		ipcErrs = make(chan error)
		// NOTE [placeholder]: This build is never busy.
		// The ipc env should be used to query activity when implemented.
		checkIfBusy = func() (bool, error) { return false, nil }
	)
	go func() {
		idleCheckTicker := time.NewTicker(checkInterval)
		defer idleCheckTicker.Stop()
		defer close(ipcErrs)
		for {
			select {
			case <-idleCheckTicker.C:
				busy, err := checkIfBusy()
				if err == nil {
					if busy {
						continue
					}
					if err = ipcEnv.Daemon().Stop(daemon.Idle); err == nil {
						return
					}
				}
				select {
				case ipcErrs <- err:
				case <-ctx.Done():
				}
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return ipcErrs
}
