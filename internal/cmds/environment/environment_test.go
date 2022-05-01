package cmdsenv_test

import (
	"context"
	"testing"

	cmdsenv "github.com/djdv/go-filesystem-utils/internal/cmds/environment"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

func makeEnv(t *testing.T) (cmdsenv.Environment, context.CancelFunc) {
	t.Helper()
	var (
		ctx, cancel = context.WithCancel(context.Background())
		root        = new(cmds.Command)
	)
	request, err := cmds.NewRequest(ctx, nil, nil,
		nil, nil, root)
	if err != nil {
		cancel()
		t.Fatal(err)
	}

	env, err := cmdsenv.MakeEnvironment(ctx, request)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	serviceEnv, err := cmdsenv.Assert(env)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return serviceEnv, cancel
}

func TestEnvironment(t *testing.T) {
	_, cancel := makeEnv(t)
	defer cancel()
}

func TestAssert(t *testing.T) {
	_, cancel := makeEnv(t)
	defer cancel()
	if _, err := cmdsenv.Assert(nil); err == nil {
		t.Fatal("expected assert to error (nil input), but got nil error")
	}
}
