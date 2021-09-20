package executor_test

import (
	"context"
	"errors"
	"testing"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/executor"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

func TestExecutor(t *testing.T) {
	t.Run("nil inputs", func(t *testing.T) {
		defer func() { recover() }() // nil args are expected to panic.
		_, err := executor.MakeExecutor(nil, nil)
		if err == nil {
			t.Fatal("executor was returned with nil constructor arguments")
		}
	})

	const (
		localCmdName  = "local"
		remoteCmdName = "remote"
	)

	var (
		ctx  = context.Background()
		root = &cmds.Command{
			Subcommands: map[string]*cmds.Command{
				localCmdName:  {NoRemote: true},
				remoteCmdName: {NoLocal: true},
			},
		}
		optMap = cmds.OptMap{
			// NOTE: We provide an address just to prevent the executor-constructor
			// from trying to spawn a server instance (which happens by default)
			fscmds.ServiceMaddrs().CommandLine(): []multiaddr.Multiaddr{
				multiaddr.StringCast("/ip4/127.0.0.1/tcp/1234"),
			},
		}
	)
	for _, test := range []struct {
		name    string
		cmdPath []string
	}{
		{
			name:    "root",
			cmdPath: nil,
		},
		{
			name:    localCmdName,
			cmdPath: []string{localCmdName},
		},
		{
			name:    remoteCmdName,
			cmdPath: []string{remoteCmdName},
		},
	} {
		t.Run(test.name+" request", func(t *testing.T) {
			request, err := cmds.NewRequest(ctx, test.cmdPath, optMap,
				nil, nil, root)
			if err != nil {
				t.Fatal(err)
			}

			fsEnv, err := environment.MakeEnvironment(ctx, request)
			if err != nil {
				t.Fatal(err)
			}

			if _, err = executor.MakeExecutor(request, fsEnv); err != nil {
				// NOTE: We're expecting to try the provided maddr,
				// but we don't expect to connect to it.
				if !errors.Is(err, executor.ErrCouldNotConnect) {
					t.Fatal(err)
				}
			}
		})
	}
}
