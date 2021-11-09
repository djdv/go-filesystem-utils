package environment_test

import (
	"context"
	"testing"

	"github.com/djdv/go-filesystem-utils/cmd/environment"
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

	if _, err := environment.MakeEnvironment(ctx, request); err != nil {
		t.Fatal(err)
	}

	/* TODO: lint or assert with topmost env
	if _, err := environment.CastEnvironment(env); err != nil {
		t.Fatal(err)
	}

	if _, err := environment.CastEnvironment(nil); err == nil {
		t.Fatal("expected to error but did not - nil environment provided")
	}
	*/
}
