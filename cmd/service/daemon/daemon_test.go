package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment"
	daemonenv "github.com/djdv/go-filesystem-utils/cmd/ipc/environment/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/service"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

func TestDaemonRun(t *testing.T) {
	root := &cmds.Command{
		Options: fscmds.RootOptions(),
		Subcommands: map[string]*cmds.Command{
			service.Name: service.Command,
		},
	}
	t.Run("Direct stop", func(t *testing.T) {
		testDaemonStop(t, root)
	})

	t.Run("Remote calls", func(t *testing.T) {
		testDaemonRemote(t, root)
	})

	t.Run("Find server instance", func(t *testing.T) {
		testFindServer(t, root)
	})
}

func testDaemonStop(t *testing.T, root *cmds.Command) {
	t.Run("Context", func(t *testing.T) {
		testDaemonCancelCtx(t, root)
	})
	t.Run("Auto exit", func(t *testing.T) {
		testDaemonAutoExit(t, root)
	})
}

func testDaemonCancelCtx(t *testing.T, root *cmds.Command) {
	var (
		ctx           = context.Background()
		expectedError = context.Canceled
		check         = func(serverCtx context.Context, response cmds.Response) {
			t.Helper()
			for {
				_, err := response.Next()
				switch {
				case err == nil:
					continue
				case errors.Is(err, expectedError):
					<-serverCtx.Done() // Wait for server to return.
					return
				default:
					t.Fatalf("server returned unexpected error"+
						"\n\twanted: %s"+
						"\n\tgot: %s",
						expectedError, err)
				}
			}
		}
	)

	t.Run("Cancel early", func(t *testing.T) {
		var (
			serverCtx         context.Context
			serverResponse    cmds.Response
			runCtx, runCancel = context.WithCancel(ctx)
		)
		t.Run("Cancel context", func(*testing.T) {
			runCancel()
		})
		t.Run("Spawn server", func(t *testing.T) {
			serverCtx, _, serverResponse = spawnDaemon(runCtx, t, root, nil)
		})
		t.Run("Check response", func(*testing.T) {
			check(serverCtx, serverResponse)
		})
	})

	t.Run("Cancel late", func(t *testing.T) {
		const serviceWait = time.Microsecond
		var (
			serverCtx         context.Context
			serverResponse    cmds.Response
			runCtx, runCancel = context.WithCancel(ctx)
		)
		t.Run("Spawn server", func(t *testing.T) {
			serverCtx, _, serverResponse = spawnDaemon(runCtx, t, root, nil)
		})
		t.Run(fmt.Sprintf("Cancel context after %s", serviceWait), func(*testing.T) {
			go func() { time.Sleep(serviceWait); runCancel() }()
		})
		t.Run("Check response", func(*testing.T) {
			check(serverCtx, serverResponse)
		})
	})
}

func testDaemonRemote(t *testing.T, root *cmds.Command) {
	var (
		serverCtx         context.Context
		fsEnv             environment.Environment
		serverResponse    cmds.Response
		ctx               = context.Background()
		runCtx, runCancel = context.WithCancel(ctx)
	)
	defer runCancel()
	t.Run("Spawn server", func(t *testing.T) {
		serverCtx, fsEnv, serverResponse = spawnDaemon(runCtx, t, root, nil)
	})

	startup, runtime := daemonenv.SplitResponse(ctx, serverResponse, nil, nil)
	if err := startup(); err != nil {
		t.Fatal(err)
	}

	var serverMaddr multiaddr.Multiaddr
	t.Run("Find server", func(t *testing.T) {
		serverMaddr = daemonFindServer(t)
	})

	var client cmds.Executor
	t.Run("Make client", func(t *testing.T) {
		var err error
		if client, err = ipc.GetClient(serverMaddr); err != nil {
			t.Fatal(err)
		}
	})

	const remoteOnly = "remote access disabled for this command"
	for _, test := range []struct {
		commandPath []string
		errorReason string
	}{
		{
			commandPath: []string{service.Name},
			errorReason: remoteOnly,
		},
		{
			commandPath: []string{service.Name, daemon.Name},
			errorReason: remoteOnly,
		},
		{
			commandPath: []string{service.Name, daemon.Name, stop.Name},
		},
	} {
		t.Run(fmt.Sprintf("Execute \"%s\"",
			strings.Join(test.commandPath, " "),
		), func(t *testing.T) {
			var (
				shouldError               = test.errorReason != ""
				requestCtx, requestCancel = context.WithCancel(ctx)
			)
			defer requestCancel()

			request, err := cmds.NewRequest(requestCtx, test.commandPath, nil, nil, nil, root)
			if err != nil {
				t.Fatal(err)
			}

			var (
				emitter, response = cmds.NewChanResponsePair(request)
				execErr           = client.Execute(request, emitter, fsEnv)
			)
			if shouldError {
				if execErr == nil {
					t.Fatalf("expected server to error but didn't (%s)", test.errorReason)
				}
				return // We're not going to get a response.

			}
			if execErr != nil {
				t.Fatalf("server returned unexpected error: %s", execErr)
			}

			if _, err := response.Next(); !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
		})
	}

	t.Run("Wait for exit", func(t *testing.T) {
		if err := runtime(); err != nil {
			t.Fatal(err)
		}
		const testGrace = 1 * time.Second
		select {
		case <-serverCtx.Done():
		case <-time.After(testGrace):
			t.Fatalf("daemon did not stop in time: %s",
				testGrace)
		}
	})
}

func testDaemonAutoExit(t *testing.T, root *cmds.Command) {
	const (
		stopAfter = time.Nanosecond
		testGrace = stopAfter + 1*time.Second
	)
	var (
		serverCtx         context.Context
		serverResponse    cmds.Response
		ctx               = context.Background()
		runCtx, runCancel = context.WithCancel(ctx)
	)
	defer runCancel()

	t.Run("Spawn server", func(t *testing.T) {
		serverCtx, _, serverResponse = spawnDaemon(runCtx, t, root,
			cmds.OptMap{
				fscmds.AutoExitInterval().CommandLine(): stopAfter.String(),
			})
	})

	var (
		responseSequence int
		sequenceChecker  = func(response *daemonenv.Response) error {
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
				t.Log(response)
			case 1:
				// Server should then tell us it's stopping, and why.
				expected := daemonenv.Response{
					Status:     daemonenv.Stopping,
					StopReason: daemonenv.Idle,
				}
				if *response != expected {
					return fmt.Errorf("Bad response sequence [%d]"+
						"\n\texpected: %#v"+
						"\n\tgot: %#v",
						responseSequence,
						expected, response)
				}
				t.Log(response)
			default:
				// Server should not be active anymore.
				return fmt.Errorf("Bad response sequence [%d]"+
					"\n\tdid not expect any more responses"+
					"\n\tgot: %#v",
					responseSequence, response)
			}
			responseSequence++
			return nil
		}

		startup, runtime = daemonenv.SplitResponse(ctx, serverResponse, nil, sequenceChecker)
	)

	t.Run("Check server startup", func(t *testing.T) {
		if err := startup(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Check runtime response sequence", func(t *testing.T) {
		if err := runtime(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Wait for server to return", func(t *testing.T) {
		select {
		case <-serverCtx.Done():
		case <-time.After(testGrace):
			t.Fatalf("server did not stop in time: %s",
				testGrace)
		}
	})
}

func testFindServer(t *testing.T, root *cmds.Command) {
	const shouldntName = "Shouldn't be found"
	t.Run(shouldntName, daemonDontFindServer)
	defer t.Run(shouldntName, daemonDontFindServer)

	var (
		ctx               = context.Background()
		runCtx, runCancel = context.WithCancel(ctx)

		serverCtx context.Context
		fsEnv     environment.Environment

		startup, runtime func() error
	)
	defer runCancel()

	t.Run("Spawn server", func(t *testing.T) {
		var serverResponse cmds.Response
		serverCtx, fsEnv, serverResponse = spawnDaemon(runCtx, t, root, nil)
		startup, runtime = daemonenv.SplitResponse(ctx, serverResponse, nil, nil)
		if err := startup(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Find server", func(t *testing.T) {
		daemonFindServer(t)
	})

	t.Run("Stop server", func(t *testing.T) {
		daemonEnv := fsEnv.Daemon()
		stopDaemonAndWait(t, daemonEnv, runtime, serverCtx)
	})
}
