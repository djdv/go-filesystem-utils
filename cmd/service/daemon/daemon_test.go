package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment/daemon"
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
	t.Run("Stop", func(t *testing.T) {
		daemonStop(t, root)
		daemonFindServer(t, root)
	})
}

func daemonStop(t *testing.T, root *cmds.Command) {
	t.Run("Context", func(t *testing.T) {
		daemonCancelCtx(t, root)
		daemonStopCmd(t, root)
	})
}

func daemonCancelCtx(t *testing.T, root *cmds.Command) {
	var (
		ctx   = context.Background()
		check = func(t *testing.T, runChan <-chan error,
			response cmds.Response, expectedError error) {
			t.Helper()
			if runErr := <-runChan; runErr != nil {
				t.Fatal(runErr)
			}

			_, err := response.Next()
			if !errors.Is(err, expectedError) {
				t.Fatalf("daemon returned unexpected error\n\twanted: %s\n\tgot: %s",
					expectedError, err)
			}
		}
	)

	t.Run("Early", func(t *testing.T) {
		runCtx, runCancel := context.WithCancel(ctx)
		runCancel()

		runErrs, response := startService(runCtx, t, root, nil)

		check(t, runErrs, response, context.Canceled)
	})

	t.Run("Late", func(t *testing.T) {
		const serviceWait = time.Microsecond
		runCtx, runCancel := context.WithCancel(ctx)

		go func() { time.Sleep(serviceWait); runCancel() }()

		runErrs, response := startService(runCtx, t, root, nil)

		check(t, runErrs, response, context.Canceled)
	})
}

func daemonStopCmd(t *testing.T, root *cmds.Command) {
	var (
		ctx                   = context.Background()
		daemonNoopInitHandler = func(daemonResponses <-chan daemon.Response,
			daemonErrs <-chan error) {
			t.Helper()
			for {
				select {
				case response := <-daemonResponses:
					if response.Status == daemon.Ready {
						return
					}
				case err := <-daemonErrs:
					t.Fatal(err)
				}
			}
		}
	)

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

		runErrs, response := startService(runCtx, t, root,
			cmds.OptMap{
				fscmds.AutoExitInterval().CommandLine(): stopAfter.String(),
			})

		daemonResponses, daemonErrs := daemon.ParseResponse(ctx, response)

		if err := daemonNoopInitHandler(daemonResponses, daemonErrs); err != nil {
			t.Fatal(err)
		}

		var responsesSeen int
		for {
			select {
			case response := <-daemonResponses:
				switch responsesSeen {
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
						Status:     daemon.Stopping,
						StopReason: daemon.Idle,
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
			case err, ok := <-daemonErrs:
				if ok {
					t.Fatal(err)
				}
			}
		}

		for range []<-chan error{responseChan, runErrs} {
			select {
			case err := <-responseChan:
				if err != nil {
					t.Fatalf("daemon response error: %s", err)
				}
			case <-time.After(testGrace):
				t.Fatal(timeoutErr)
			case err = <-runErrs:
				if err != nil {
					t.Fatalf("daemon returned unexpected error: %s", err)
				}
			}
		}
	})
}

func startService(ctx context.Context, t *testing.T,
	root *cmds.Command, optMap cmds.OptMap) (<-chan error, cmds.Response) {
	request, err := cmds.NewRequest(ctx, fscmds.DaemonCmdsPath(),
		optMap, nil, nil, root)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := environment.MakeEnvironment(ctx, request)
	if err != nil {
		t.Fatal(err)
	}

	var (
		cmdRunErrs        = make(chan error, 1)
		emitter, response = cmds.NewChanResponsePair(request)
	)

	go func() { defer close(cmdRunErrs); cmdRunErrs <- root.Call(request, emitter, environment) }()

	return cmdRunErrs, response
}

func daemonFindServer(t *testing.T, root *cmds.Command) {
	t.Run("Find server", func(t *testing.T) {
		{
			serverMaddr, err := ipc.FindLocalServer()
			if !errors.Is(err, ipc.ErrServiceNotFound) {
				t.Fatal("did not expect to find server, but did:", serverMaddr)
			}
		}
		var (
			ctx               = context.Background()
			runCtx, runCancel = context.WithCancel(ctx)
		)
		defer runCancel()

		runErrs, response, err := startService(runCtx, root, nil)
		if err != nil {
			t.Fatal(err)
		}

		if err := daemon.HandleInitSequence(response, noopParseFunc); err != nil {
			t.Fatal(err)
		}

		serverMaddr, err := ipc.FindLocalServer()
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
		case err := <-runErrs:
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
