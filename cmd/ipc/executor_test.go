package ipc_test

import (
	"context"
	"testing"

	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

func TestExecutor(t *testing.T) {
	t.Run("nil inputs", func(t *testing.T) {
		defer func() { recover() }() // nil args are expected to panic.
		_, err := ipc.MakeExecutor(nil, nil)
		if err == nil {
			t.Fatal("executor was returned with nil constructor arguments")
		}
	})

	var (
		ctx  = context.Background()
		root = &cmds.Command{}
	)
	request, err := cmds.NewRequest(ctx, nil, nil,
		nil, nil, root)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Regular request", func(t *testing.T) {
		fsEnv, err := ipc.MakeEnvironment(ctx, request)
		if err != nil {
			t.Fatal(err)
		}

		if _, err = ipc.MakeExecutor(request, fsEnv); err != nil {
			t.Fatal(err)
		}
	})

	// TODO: type check return from MakeExecutor
	// NoLocal requests should be of type *http.clients
	// If not, fail.
}
