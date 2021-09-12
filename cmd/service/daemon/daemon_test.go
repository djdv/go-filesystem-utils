package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/executor"
	"github.com/djdv/go-filesystem-utils/cmd/service"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

func TestDaemonRun(t *testing.T) {
	root := &cmds.Command{
		Options: fscmds.RootOptions(),
		Subcommands: map[string]*cmds.Command{
			service.Name: service.Command,
		},
	}

	// Just consume the values to unblock, we don't care about the responses themselves.
	var noopParseFunc daemon.ResponseHandlerFunc = func(*daemon.Response) error { return nil }

	t.Run("Cancel", func(t *testing.T) {
		var (
			ctx   = context.Background()
			check = func(t *testing.T, daemonChan <-chan error,
				response cmds.Response, expectedError error) {
				t.Helper()
				if daemonErr := <-daemonChan; daemonErr != nil {
					t.Fatal(daemonErr)
				}

				_, err := response.Next()
				if !errors.Is(err, expectedError) {
					t.Fatalf("daemon returned unexpected error\n\twanted: %s\n\tgot: %s",
						expectedError, err)
				}
			}
		)
		t.Run("Context", func(t *testing.T) {
			t.Run("Early", func(t *testing.T) {
				runCtx, runCancel := context.WithCancel(ctx)
				runCancel()

				execErrs, response, err := startService(runCtx, root, nil)
				if err != nil {
					t.Fatal(err)
				}

				check(t, execErrs, response, context.Canceled)
			})
			t.Run("Late", func(t *testing.T) {
				const serviceWait = time.Microsecond
				runCtx, runCancel := context.WithCancel(ctx)
				go func() {
					time.Sleep(serviceWait)
					runCancel()
				}()

				execErrs, response, err := startService(runCtx, root, nil)
				if err != nil {
					t.Fatal(err)
				}

				check(t, execErrs, response, context.Canceled)
			})
		})
		t.Run("Auto shutdown", func(t *testing.T) {
			const (
				stopAfter = time.Nanosecond
				testGrace = stopAfter + 1*time.Second
			)
			var (
				timeoutErr        = fmt.Errorf("daemon did not stop in time: %s", testGrace)
				runCtx, runCancel = context.WithCancel(ctx)
			)
			defer runCancel()
			execErrs, response, err := startService(runCtx, root,
				cmds.OptMap{
					fscmds.AutoExitInterval().CommandLine(): stopAfter.String(),
				})
			if err != nil {
				t.Fatal(err)
			}

			if err := daemon.HandleInitSequence(response, noopParseFunc); err != nil {
				t.Fatal(err)
			}

			var (
				responseSequence int
				autoExitHandler  daemon.ResponseHandlerFunc = func(response *daemon.Response) error {
					t.Helper()
					switch responseSequence {
					case 0:
						// Server should respond, telling us it's going to
						// stop on idle (with the interval).
						if response.Info == "" {
							return fmt.Errorf("Bad response sequence [%d]"+
								"expected Response.Info to be populated"+
								"\n\tgot: %#v",
								responseSequence, response)
						}
					case 1:
						// Server should tell us it's stopping, and why.
						expected := daemon.Response{
							Status: daemon.Stopping,
							Reason: daemon.Idle,
						}
						if *response != expected {
							return fmt.Errorf("Bad response sequence [%d]"+
								"\n\texpected: %#v"+
								"\n\tgot: %#v",
								responseSequence,
								expected, response)
						}
					default:
						// Server should not be active anymore.
						return fmt.Errorf("Bad response sequence, expecting Response.Info"+
							"\n\tgot: %#v", response)
					}
					responseSequence++
					return nil
				}
			)

			responseChan := make(chan error, 1)
			go func() {
				responseChan <- daemon.HandleRunningSequence(response, autoExitHandler)
			}()

			for range []<-chan error{responseChan, execErrs} {
				select {
				case err := <-responseChan:
					if err != nil {
						t.Fatalf("daemon response error: %s", err)
					}
				case <-time.After(testGrace):
					t.Fatal(timeoutErr)
				case err = <-execErrs:
					if err != nil {
						t.Fatalf("daemon returned unexpected error: %s", err)
					}
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
			ctx               = context.Background()
			runCtx, runCancel = context.WithCancel(ctx)
		)
		defer runCancel()

		execErrs, response, err := startService(runCtx, root, nil)
		if err != nil {
			t.Fatal(err)
		}

		if err := daemon.HandleInitSequence(response, noopParseFunc); err != nil {
			t.Fatal(err)
		}

		serverMaddr, err := fscmds.FindLocalServer()
		if err != nil {
			t.Fatal("expected to find server, but didn't:", err)
		}
		if serverMaddr == nil {
			t.Fatal("server finder returned no error, but also no server")
		}
		runCancel()

		responseChan := make(chan error, 1)
		go func() {
			responseChan <- daemon.HandleRunningSequence(response, noopParseFunc)
		}()

		select {
		case err := <-execErrs:
			if err != nil {
				t.Fatal("unexpected service error:", err)
			}
		case err := <-responseChan:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("unexpected response error:", err)
			}
		}
	})
}

func startService(ctx context.Context,
	cmdsRoot *cmds.Command, optMap cmds.OptMap) (<-chan error, cmds.Response, error) {
	// TODO: use the function to get the path
	//request, err := cmds.NewRequest(ctx, []string{service.Name},
	request, err := cmds.NewRequest(ctx, []string{"service", "daemon"},
		optMap, nil, nil, cmdsRoot)
	if err != nil {
		return nil, nil, err
	}
	environment, err := environment.MakeEnvironment(ctx, request)
	if err != nil {
		return nil, nil, err
	}
	executor, err := executor.MakeExecutor(request, environment)
	if err != nil {
		return nil, nil, err
	}

	var (
		execChan          = make(chan error, 1)
		emitter, response = cmds.NewChanResponsePair(request)
	)
	go func() { defer close(execChan); execChan <- executor.Execute(request, emitter, environment) }()

	return execChan, response, nil
}
