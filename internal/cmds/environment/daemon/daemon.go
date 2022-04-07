package daemon

import (
	"context"

	"github.com/djdv/go-filesystem-utils/internal/cmds/environment/mount"
)

type (
	Daemon interface {
		Mounter() mount.Mounter
	}
	daemon struct {
		context.Context
		mounter mount.Mounter
	}
)

func New(ctx context.Context) *daemon {
	return &daemon{Context: ctx}
}

func (env *daemon) Mounter() mount.Mounter {
	mounter := env.mounter
	if mounter == nil {
		mounter = mount.New(env.Context)
		env.mounter = mounter
	}
	return mounter
}
