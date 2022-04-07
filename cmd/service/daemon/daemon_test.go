package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djdv/go-filesystem-utils/cmd/service"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	fscmds "github.com/djdv/go-filesystem-utils/internal/cmds/settings"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/environment"
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
	t.Run("Cancel", func(t *testing.T) {
		t.Run("now", func(t *testing.T) {
			var (
				expectedErr = context.Canceled
				ctx, cancel = context.WithCancel(context.Background())
			)
			cancel()
			testCtx(t, expectedErr, root, ctx)
		})
		const serviceWait = time.Nanosecond
		t.Run(fmt.Sprintf("after %s", serviceWait), func(t *testing.T) {
			var (
				expectedErr = context.DeadlineExceeded
				ctx, cancel = context.WithTimeout(context.Background(), serviceWait)
			)
			defer cancel()
			testCtx(t, expectedErr, root, ctx)
		})
	})
}

func testCtx(t *testing.T, expectedError error, root *cmds.Command, ctx context.Context) {
	var (
		check = func(t *testing.T, startup, runtime func() error, expectedError error) {
			// The expected error may come back to us during startup or runtime.
			// It depends mainly on the runtime scheduler and how far along
			// the daemon process got before the context was canceled+checked.
			var err error
			for _, fn := range []func() error{startup, runtime} {
				if e := fn(); e != nil {
					if err == nil {
						err = e
					} else {
						err = fmt.Errorf("%w\n\t%s", err, e)
					}
				}
			}
			if !errors.Is(err, expectedError) {
				t.Fatalf("server returned unexpected error"+
					"\n\twanted: %s"+
					"\n\tgot: %s",
					expectedError, err)
			}
		}
		serverCtx      context.Context
		serverResponse cmds.Response
	)
	t.Run("Spawn server", func(t *testing.T) {
		serverCtx, _, serverResponse = spawnDaemon(ctx, t, root, nil)
	})
	t.Run("Check response", func(t *testing.T) {
		startup, runtime := daemon.SplitResponse(serverResponse, nil, nil)
		check(t, startup, runtime, expectedError)
	})
	t.Run("Wait for server to return", func(t *testing.T) {
		waitForDaemon(t, serverCtx)
	})
	t.Run("Check files", func(t *testing.T) {
		checkHostEnv(t)
	})
}

func testDaemonRemote(t *testing.T, root *cmds.Command) {
	for _, test := range []struct {
		serverOptions cmds.OptMap
		name          string
	}{
		{
			name:          "defaults",
			serverOptions: nil,
		},
		{
			name: "tcp servers",
			serverOptions: cmds.OptMap{
				fscmds.APIParam().CommandLine(): []string{
					"/ip4/127.0.0.1/tcp/0",
					"/dns4/localhost/tcp/0",
				},
			},
		},
		{
			name: "unix domain socket servers",
			serverOptions: cmds.OptMap{
				fscmds.APIParam().CommandLine(): []string{
					path.Join("/unix/", filepath.Join(os.TempDir(), "test-socket")),
				},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var (
				serverCtx      context.Context
				serviceEnv     environment.Environment
				serverResponse cmds.Response

				ctx               = context.Background()
				runCtx, runCancel = context.WithCancel(ctx)
				options           = test.serverOptions
			)
			defer runCancel()
			t.Run("Spawn server", func(t *testing.T) {
				serverCtx, serviceEnv, serverResponse = spawnDaemon(runCtx, t, root, options)
			})

			var (
				startupCb   daemon.ResponseCallback
				serverMaddr multiaddr.Multiaddr
			)
			if options != nil {
				startupCb = func(r *daemon.Response) error {
					if maddr := r.ListenerMaddr; maddr != nil {
						serverMaddr = maddr
					}
					return nil
				}
			}
			startup, runtime := daemon.SplitResponse(serverResponse, startupCb, nil)
			if err := startup(); err != nil {
				t.Fatal(err)
			}

			if options == nil {
				t.Run("Finding default server",
					func(t *testing.T) { serverMaddr = daemonFindServer(t) },
				)
			}

			var client cmds.Executor
			t.Run(fmt.Sprintf("Make client for: %s", serverMaddr.String()),
				func(t *testing.T) {
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
				waitForDaemon(t, serverCtx)
			})
		})
	}
}

func daemonRemoteHelper(t *testing.T, root *cmds.Command,
	cmdPath []string, errorReason string,
	client cmds.Executor, env cmds.Environment,
) {
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
				fscmds.AutoExitParam().CommandLine(): stopAfter.String(),
			})
	})

	var (
		sawExpected bool
		expected    = daemon.Response{
			Status:     daemon.Stopping,
			StopReason: environment.Idle,
		}
		idleChecker = func(response *daemon.Response) error {
			if *response == expected {
				sawExpected = true
			}
			return nil
		}
		startup, runtime = daemon.SplitResponse(serverResponse, nil, idleChecker)
	)
	if err := startup(); err != nil {
		t.Error("server failed startup checks:", err)
	}
	if err := runtime(); err != nil {
		t.Error("server failed runtime checks:", err)
	}
	if !sawExpected {
		t.Errorf("server never emitted expected startup response - wanted:\"%s\"", expected.String())
	}

	t.Run("Wait for server to return", func(t *testing.T) {
		waitForDaemon(t, serverCtx)
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
		serviceEnv environment.Environment

		startup, runtime func() error
	)
	defer runCancel()

	t.Run("Spawn server", func(t *testing.T) {
		var serverResponse cmds.Response
		serverCtx, serviceEnv, serverResponse = spawnDaemon(runCtx, t, root, nil)
		startup, runtime = daemon.SplitResponse(serverResponse, nil, nil)
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
