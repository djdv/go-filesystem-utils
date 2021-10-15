package daemon_test

import (
	"context"
	"errors"
	"testing"
	"time"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment"
	daemonenv "github.com/djdv/go-filesystem-utils/cmd/ipc/environment/daemon"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

// spawnDaemon sets up the daemon environment and starts the server daemon.
// The returned context is done when the daemon returns.
func spawnDaemon(ctx context.Context, t *testing.T,
	root *cmds.Command, optMap cmds.OptMap) (context.Context, environment.Environment, cmds.Response) {
	t.Helper()
	request, err := cmds.NewRequest(ctx, fscmds.DaemonCmdsPath(),
		optMap, nil, nil, root)
	if err != nil {
		t.Fatal(err)
	}

	env, err := environment.MakeEnvironment(ctx, request)
	if err != nil {
		t.Fatal(err)
	}

	fsEnv, err := environment.CastEnvironment(env)
	if err != nil {
		t.Fatal(err)
	}

	var (
		serverCtx, serverCancel = context.WithCancel(context.Background())
		emitter, response       = cmds.NewChanResponsePair(request)
	)
	go func() {
		defer serverCancel()
		root.Call(request, emitter, env)
	}()

	return serverCtx, fsEnv, response
}

func daemonFindServer(t *testing.T) (serverMaddr multiaddr.Multiaddr) {
	t.Helper()
	var err error
	if serverMaddr, err = ipc.FindLocalServer(); err != nil {
		t.Fatal("expected to find server, but didn't:", err)
	}
	if serverMaddr == nil {
		t.Fatal("server finder returned no error, but also no server")
	}
	return
}

func daemonDontFindServer(t *testing.T) {
	t.Helper()
	serverMaddr, err := ipc.FindLocalServer()
	if !errors.Is(err, ipc.ErrServiceNotFound) {
		t.Fatal("did not expect to find server, but did:", serverMaddr)
	}
}

func stopDaemon(t *testing.T, daemonEnv daemonenv.Environment) {
	t.Helper()
	if err := daemonEnv.Stop(daemonenv.StopRequested); err != nil {
		t.Fatal(err)
	}
}

func stopDaemonAndWait(t *testing.T,
	daemonEnv daemonenv.Environment, runtime func() error, serverCtx context.Context) {
	t.Helper()
	stopDaemon(t, daemonEnv)

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
}
