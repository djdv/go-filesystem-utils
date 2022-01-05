package environment_test

import (
	"context"
	"testing"

	"github.com/djdv/go-filesystem-utils/cmd/environment"
	serviceenv "github.com/djdv/go-filesystem-utils/cmd/environment"
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
}

func TestAssert(t *testing.T) {
	env := serviceenv.MakeEnvironment()

	if _, err := serviceenv.Assert(env); err != nil {
		t.Fatal(err)
	}

	if _, err := serviceenv.Assert(nil); err == nil {
		t.Fatal("expected assert to error (nil input), but got nil error")
	}
}
