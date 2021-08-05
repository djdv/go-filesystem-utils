package ipc_test

import (
	"context"
	"testing"

	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

func TestEnvironment(t *testing.T) {
	var (
		ctx  = context.Background()
		root = &cmds.Command{}
	)
	request, err := cmds.NewRequest(ctx, nil, nil,
		nil, nil, root)
	if err != nil {
		t.Fatal(err)
	}

	env, err := ipc.MakeEnvironment(ctx, request)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ipc.CastEnvironment(env); err != nil {
		t.Fatal(err)
	}
}
