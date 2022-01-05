package daemon_test

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/djdv/go-filesystem-utils/cmd/environment"
	serviceenv "github.com/djdv/go-filesystem-utils/cmd/environment"
	daemonenv "github.com/djdv/go-filesystem-utils/cmd/service/daemon/env"
	stopenv "github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop/env"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

// spawnDaemon sets up the daemon environment and starts the server daemon.
// The returned context is done when the daemon returns.
func spawnDaemon(ctx context.Context, t *testing.T,
	root *cmds.Command, optMap cmds.OptMap) (context.Context, serviceenv.Environment, cmds.Response) {
	t.Helper()
	request, err := cmds.NewRequest(ctx, daemon.CmdsPath(),
		optMap, nil, nil, root)
	if err != nil {
		t.Log("hit:", err)
		t.Fatal(err)
	}

	env, err := environment.MakeEnvironment(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	serviceEnv, err := serviceenv.Assert(env)
	if err != nil {
		t.Fatal(err)
	}

	var (
		emitter, response       = cmds.NewChanResponsePair(request)
		serverCtx, serverCancel = context.WithCancel(context.Background())
	)
	go func() {
		defer serverCancel()
		root.Call(request, emitter, env)
	}()

	return serverCtx, serviceEnv, response
}

func daemonFindServer(t *testing.T) (serverMaddr multiaddr.Multiaddr) {
	t.Helper()
	var err error
	if serverMaddr, err = daemon.FindLocalServer(); err != nil {
		t.Fatal("expected to find server, but didn't:", err)
	}
	if serverMaddr == nil {
		t.Fatal("server finder returned no error, but also no server")
	}
	return
}

func daemonDontFindServer(t *testing.T) {
	t.Helper()
	serverMaddr, err := daemon.FindLocalServer()
	if !errors.Is(err, daemon.ErrServiceNotFound) {
		t.Fatal("did not expect to find server, but did:", serverMaddr)
	}
}

func stopDaemon(t *testing.T, daemonEnv daemonenv.Environment) {
	t.Helper()
	if err := daemonEnv.Stopper().Stop(stopenv.Requested); err != nil {
		t.Fatal(err)
	}
}

func waitForDaemon(t *testing.T, serverCtx context.Context) {
	t.Helper()
	const testGrace = 64 * time.Millisecond
	select {
	case <-serverCtx.Done():
	case <-time.After(testGrace):
		t.Fatalf("server did not stop in time: %s",
			testGrace)
	}
}

func stopDaemonAndWait(t *testing.T,
	daemonEnv daemonenv.Environment, runtime func() error, serverCtx context.Context) {
	t.Helper()
	stopDaemon(t, daemonEnv)
	if err := runtime(); err != nil {
		t.Fatal(err)
	}
	waitForDaemon(t, serverCtx)
}

func checkHostEnv(t *testing.T) {
	t.Helper()
	systemMaddrs, err := daemon.SystemServiceMaddrs()
	if err != nil {
		t.Fatal(err)
	}
	userMaddrs, err := daemon.UserServiceMaddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, maddr := range append(systemMaddrs, userMaddrs...) {
		if path := getFirstUnixSocketPath(maddr); path != "" {
			if _, err := os.Stat(path); err == nil {
				t.Errorf(
					"socket path exists (should have been cleaned up on daemon shutdown): \"%s\"",
					path)
			}
		}
	}
}

func getFirstUnixSocketPath(ma multiaddr.Multiaddr) (target string) {
	multiaddr.ForEach(ma, func(comp multiaddr.Component) bool {
		isUnixComponent := comp.Protocol().Code == multiaddr.P_UNIX
		if isUnixComponent {
			target = comp.Value()
			if runtime.GOOS == "windows" { // `/C:\path` -> `C:\path`
				target = strings.TrimPrefix(target, `/`)
			}
			return true
		}
		return false
	})
	return
}
