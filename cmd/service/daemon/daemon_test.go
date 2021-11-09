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
	serviceenv "github.com/djdv/go-filesystem-utils/cmd/environment/service"
	stopenv "github.com/djdv/go-filesystem-utils/cmd/environment/service/daemon/stop"
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

	// FIXME: this fails sometimes. Why?
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
		/*
			go func() {
				time.Sleep(serviceWait)
				t.Run(fmt.Sprintf(
					"Context canceled after %s", serviceWait),
					func(*testing.T) { runCancel() },
				)
			}()
		*/
		t.Run(fmt.Sprintf(
			"Cancel context after %s", serviceWait),
			func(*testing.T) {
				go func() {
					time.Sleep(serviceWait)
					runCancel()
				}()
			},
		)
		t.Run("Check response", func(*testing.T) {
			check(serverCtx, serverResponse)
		})
	})
}

func testDaemonRemote(t *testing.T, root *cmds.Command) {
	var (
		serverCtx      context.Context
		serviceEnv     serviceenv.Environment
		serverResponse cmds.Response

		ctx               = context.Background()
		runCtx, runCancel = context.WithCancel(ctx)
	)
	defer runCancel()
	t.Run("Spawn server", func(t *testing.T) {
		serverCtx, serviceEnv, serverResponse = spawnDaemon(runCtx, t, root, nil)
	})

	startup, runtime := daemon.SplitResponse(ctx, serverResponse, nil, nil)
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
		if client, err = daemon.GetClient(serverMaddr); err != nil {
			t.Fatal(err)
		}
	})

	const remoteOnly = "remote access disabled for this command"
	for _, test := range []struct {
		errorReason string
		commandPath []string
	}{
		{
			errorReason: remoteOnly,
			commandPath: []string{service.Name},
		},
		{
			errorReason: remoteOnly,
			commandPath: []string{service.Name, daemon.Name},
		},
		{
			commandPath: []string{service.Name, daemon.Name, stop.Name},
		},
	} {
		t.Run(
			fmt.Sprintf("Execute \"%s\"", strings.Join(test.commandPath, " ")),
			func(t *testing.T) {
				daemonRemoteHelper(
					t, root,
					test.commandPath, test.errorReason,
					client, serviceEnv,
				)
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

func daemonRemoteHelper(t *testing.T, root *cmds.Command,
	cmdPath []string, errorReason string,
	client cmds.Executor, env cmds.Environment) {
	t.Helper()
	var (
		shouldError               = errorReason != ""
		requestCtx, requestCancel = context.WithCancel(context.Background())
	)
	defer requestCancel()

	request, err := cmds.NewRequest(requestCtx, cmdPath, nil, nil, nil, root)
	if err != nil {
		t.Fatal(err)
	}

	var (
		emitter, response = cmds.NewChanResponsePair(request)
		execErr           = client.Execute(request, emitter, env)
	)
	if shouldError {
		if execErr == nil {
			t.Fatalf("expected server to error but didn't (%s)", errorReason)
		}
		return // We're not going to get a response.
	}
	if execErr != nil {
		t.Fatalf("server returned unexpected error: %s", execErr)
	}

	if _, err := response.Next(); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}

func testDaemonAutoExit(t *testing.T, root *cmds.Command) {
	const stopAfter = time.Nanosecond
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
		sawExpected bool
		expected    = daemon.Response{
			Status:     daemon.Stopping,
			StopReason: stopenv.Idle,
		}
		idleChecker = func(response *daemon.Response) error {
			if *response == expected {
				sawExpected = true
			}
			return nil
		}
		startup, runtime = daemon.SplitResponse(ctx, serverResponse, nil, idleChecker)
	)
	if err := startup(); err != nil {
		t.Fatal("server failed startup checks:", err)
	}
	if err := runtime(); err != nil {
		t.Fatal("server failed runtime checks:", err)
	}
	if !sawExpected {
		t.Fatal("server never emitted expected response:", expected)
	}

	const testGrace = stopAfter + 1*time.Second
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

		serverCtx  context.Context
		serviceEnv serviceenv.Environment

		startup, runtime func() error
	)
	defer runCancel()

	t.Run("Spawn server", func(t *testing.T) {
		var serverResponse cmds.Response
		serverCtx, serviceEnv, serverResponse = spawnDaemon(runCtx, t, root, nil)
		startup, runtime = daemon.SplitResponse(ctx, serverResponse, nil, nil)
		if err := startup(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Find server", func(t *testing.T) {
		daemonFindServer(t)
	})

	t.Run("Stop server", func(t *testing.T) {
		stopDaemonAndWait(t, serviceEnv.Daemon(), runtime, serverCtx)
	})
}
