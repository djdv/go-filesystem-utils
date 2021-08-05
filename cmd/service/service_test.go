package service_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	"github.com/djdv/go-filesystem-utils/cmd/service"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

func TestServiceRun(t *testing.T) {
	root := &cmds.Command{
		Options: fscmds.RootOptions(),
		Subcommands: map[string]*cmds.Command{
			service.Name: service.Command,
		},
	}
	callServiceMethod := func(ctx context.Context, optMap cmds.OptMap) (<-chan error, context.CancelFunc, error) {
		request, err := cmds.NewRequest(ctx, []string{service.Name},
			optMap, nil, nil, root)
		if err != nil {
			return nil, nil, err
		}
		environment, err := ipc.MakeEnvironment(ctx, request)
		if err != nil {
			return nil, nil, err
		}
		executor, err := ipc.MakeExecutor(request, environment)
		if err != nil {
			return nil, nil, err
		}

		// HACK:
		// chanresponse's emitter will return a context error
		// if it's request context is canceled.
		// We need to test the response value
		// from the command only. Which could also be a context error.
		// So the test gets its own cancelFunc.
		var (
			testContext, testCancel = context.WithCancel(context.Background())
			testRequest             = *request

			execChan = make(chan error, 1)
			respChan = make(chan error, 1)
			testChan = make(chan error, 1)
		)
		testRequest.Context = testContext

		emitter, response := cmds.NewChanResponsePair(&testRequest)
		go func() { execChan <- executor.Execute(request, emitter, environment) }()
		go func() { _, err := response.Next(); respChan <- err }()
		go func() {
			defer close(testChan)
			for execChan != nil ||
				respChan != nil {
				select {
				case <-testContext.Done():
					testChan <- fmt.Errorf("test canceled before service returned")
					return
				case execErr := <-execChan:
					if err != nil {
						testChan <- fmt.Errorf("failed to execute service command: %w",
							execErr)
					}
					execChan = nil
				case responseErr := <-respChan:
					expectedErr := io.EOF
					if !errors.Is(responseErr, expectedErr) {
						testChan <- fmt.Errorf("service run failed\n\texpected %v\n\tgot: %w",
							expectedErr, responseErr)
					}
					respChan = nil
				}
			}
		}()

		return testChan, testCancel, nil
	}

	t.Run("Cancel", func(t *testing.T) {
		var (
			testCtx     = context.Background()
			cancelCheck = func(t *testing.T, serviceChan <-chan error, expectedError error) {
				t.Helper()
				serviceErr := <-serviceChan
				if serviceErr == nil {
					t.Fatalf("expected service to be canceled but no error was returned")
				}
				if !errors.Is(serviceErr, expectedError) {
					t.Fatalf("service returned unexpected error\n\twanted: %s\n\tgot: %s",
						expectedError, serviceErr)
				}
			}
		)
		t.Run("Context", func(t *testing.T) {
			t.Run("Early", func(t *testing.T) {
				runCtx, runCancel := context.WithCancel(testCtx)
				runCancel()

				serviceErr, testCancel, err := callServiceMethod(runCtx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer testCancel()

				cancelCheck(t, serviceErr, context.Canceled)
			})
			t.Run("Late", func(t *testing.T) {
				const serviceWait = time.Microsecond
				runCtx, runCancel := context.WithCancel(testCtx)
				go func() {
					time.Sleep(serviceWait)
					runCancel()
				}()

				serviceErr, testCancel, err := callServiceMethod(runCtx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer testCancel()

				cancelCheck(t, serviceErr, context.Canceled)
			})
		})
		t.Run("Auto shutdown", func(t *testing.T) {
			const (
				stopAfter = time.Nanosecond
				testGrace = stopAfter + (10 * time.Second)
			)

			runCtx, runCancel := context.WithCancel(testCtx)
			defer runCancel()
			serviceErr, testCancel, err := callServiceMethod(runCtx, cmds.OptMap{
				fscmds.AutoExitInterval().CommandLine(): stopAfter.String(),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer testCancel()

			select {
			case <-time.After(testGrace):
				testCancel()
				err = <-serviceErr
				t.Fatal("service process did not stop in time: ", err)
			case err = <-serviceErr:
				expectedError := context.DeadlineExceeded
				switch {
				case err == nil:
					t.Fatalf("expected service to be canceled but no error was returned")
				case !errors.Is(err, expectedError):
					t.Fatalf("service returned unexpected error\n\twanted: %s\n\tgot: %s",
						expectedError, err)
				}
			}
		})
	})
	t.Run("Find server", func(t *testing.T) {
		{
			serverMaddr, err := fscmds.FindLocalServer()
			if !errors.Is(err, fscmds.ErrServiceNotFound) {
				t.Fatal("did not expect to find server, but did:", serverMaddr)
			}
		}
		var (
			testCtx           = context.Background()
			runCtx, runCancel = context.WithCancel(testCtx)
		)
		defer runCancel()

		serviceErr, testCancel, err := callServiceMethod(runCtx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer testCancel()

		// XXX: Use of time is not acceptable.
		// Service needs to change so that it emits something when it's ready.
		time.Sleep(100 * time.Millisecond)

		serverMaddr, err := fscmds.FindLocalServer()
		if err != nil {
			t.Fatal("expected to find server, but didn't:", err)
		}
		if serverMaddr == nil {
			t.Fatal("server finder returned no error, but also no server")
		}
		runCancel()

		if err := <-serviceErr; !errors.Is(err, context.Canceled) {
			t.Fatal("unexpected service error:", err)
		}
	})
}
