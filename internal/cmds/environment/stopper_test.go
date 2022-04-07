package cmdsenv_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/environment"
)

// Make sure the stringer file was generated and works (at compile time).
func TestStringer(t *testing.T) {
	var zeroReason environment.Reason
	_ = zeroReason.String()
	_ = environment.Requested.String()
}

func TestStopper(t *testing.T) {
	var (
		ctx, cancel           = context.WithCancel(context.Background())
		serviceEnv, envCancel = makeEnv(t)
		stopEnv               = serviceEnv.Daemon().Stopper()
	)
	defer cancel()
	defer envCancel()

	t.Run("Stop should error", func(t *testing.T) {
		if err := stopEnv.Stop(environment.Requested); err == nil {
			t.Fatal("expected Stop to return error (not initialized) but received nil")
		}
	})

	var stopChan <-chan environment.Reason
	t.Run("Initialize", func(t *testing.T) {
		var err error
		if stopChan, err = stopEnv.Initialize(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := stopEnv.Initialize(ctx); err == nil {
			t.Fatal("expected Initialize to return error (initialized twice) but received nil")
		}
	})

	t.Run("Stop", func(t *testing.T) {
		var (
			testReason = environment.Requested

			stopErrs = make(chan error, 1)
			stop     = func() {
				defer close(stopErrs)
				if err := stopEnv.Stop(testReason); err != nil {
					stopErrs <- err
				}
			}

			reasonErrs    = make(chan error, 1)
			handleReasons = func() {
				defer close(reasonErrs)
				for reason := range stopChan {
					if reason != testReason {
						reasonErrs <- fmt.Errorf("stop reason doesn't match request: %v != %v",
							reason, testReason,
						)
					}
				}
			}

			fail = func(errPrefix string, err error) {
				t.Helper()
				t.Fatalf("%s encountered error: %v", errPrefix, err)
			}
		)

		go stop()
		go handleReasons()
		for stopErrs != nil ||
			reasonErrs != nil {
			select {
			case err, ok := <-stopErrs:
				if !ok {
					stopErrs = nil
					continue
				}
				fail("stop", err)
			case err, ok := <-reasonErrs:
				if !ok {
					reasonErrs = nil
					continue
				}
				fail("reason chan", err)
			}
		}
	})

	t.Run("Cancel", func(t *testing.T) {
		testCtx, testCancel := context.WithCancel(ctx)
		if _, err := stopEnv.Initialize(testCtx); err != nil {
			t.Fatal(err)
		}
		testCancel()
		var (
			err         = stopEnv.Stop(environment.Requested)
			expectedErr = context.Canceled
		)
		if !errors.Is(err, expectedErr) {
			t.Fatalf("stop returned unexpected error:"+
				"\n\texpected: `%v`"+
				"\n\tgot: `%v`",
				expectedErr, err,
			)
		}
	})
}
