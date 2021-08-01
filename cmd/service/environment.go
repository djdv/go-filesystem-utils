package service

import (
	"context"

	cmds "github.com/ipfs/go-ipfs-cmds"
)

type (
	daemonEnvironment struct {
		context.Context
	}
)

func MakeEnvironment(ctx context.Context, _ *cmds.Request) (cmds.Environment, error) {
	env := &daemonEnvironment{
		Context: ctx,
	}
	return env, nil
}
