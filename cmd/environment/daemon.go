package environment

import (
	list "github.com/djdv/go-filesystem-utils/cmd/list/env"
	mount "github.com/djdv/go-filesystem-utils/cmd/mount/env"
)

type (
	Daemon interface {
		Stopper() Stopper
		Lister() list.Environment
		Mounter() mount.Environment
	}
	daemon struct {
		stopper Stopper
		lister  list.Environment
		mounter mount.Environment
	}
)

func (env *environment) Daemon() Daemon {
	d := env.daemon
	if d == nil {
		d = new(daemon)
		env.daemon = d
	}
	return d
}

func (env *daemon) Stopper() Stopper {
	s := env.stopper
	if s == nil {
		s = new(stopper)
		env.stopper = s
	}
	return s
}

func (env *daemon) Lister() list.Environment {
	l := env.lister
	if l == nil {
		l = list.MakeEnvironment()
		env.lister = l
	}
	return l
}

func (env *daemon) Mounter() mount.Environment {
	m := env.mounter
	if m == nil {
		m = mount.MakeEnvironment()
		env.mounter = m
	}
	return m
}

func (env *daemon) Unmounter() mount.Environment {
	u := env.mounter
	if u == nil {
		u = mount.MakeEnvironment()
		env.mounter = u
	}
	return u
}
