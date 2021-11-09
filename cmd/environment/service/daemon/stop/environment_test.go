package stop_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/djdv/go-filesystem-utils/cmd/environment/service/daemon/stop"
)

// This is more of  compile time test
// to make sure the stringer file was generated and works.
func TestStringer(t *testing.T) {
	var zeroReason stop.Reason
	_ = zeroReason.String()
	_ = stop.Requested.String()
}

func TestStopper(t *testing.T) {
	var (
		ctx     = context.Background()
		stopEnv = stop.MakeEnvironment()
	)

	t.Run("Stop should error", func(t *testing.T) {
		if err := stopEnv.Stop(stop.Requested); err == nil {
			t.Fatal("expected Stop to return error (not initialized) but received nil")
		}
	})

	var stopChan <-chan stop.Reason
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
			testReason = stop.Requested

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
			err         = stopEnv.Stop(stop.Requested)
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
