package daemon

import (
	"github.com/djdv/go-filesystem-utils/cmd/environment/service/daemon/stop"
	list "github.com/djdv/go-filesystem-utils/cmd/list/env"
	mount "github.com/djdv/go-filesystem-utils/cmd/mount/env"
)

type (
	Environment interface {
		Stopper() stop.Environment
		Lister() list.Environment
		Mounter() mount.Environment
	}
	environment struct {
		stopper stop.Environment
		lister  list.Environment
		mounter mount.Environment
	}
)

func MakeEnvironment() Environment { return &environment{} }

func (env *environment) Stopper() stop.Environment {
	s := env.stopper
	if s == nil {
		s = stop.MakeEnvironment()
		env.stopper = s
	}
	return s
}

func (env *environment) Lister() list.Environment {
	l := env.lister
	if l == nil {
		l = list.MakeEnvironment()
		env.lister = l
	}
	return l
}

func (env *environment) Mounter() mount.Environment {
	m := env.mounter
	if m == nil {
		m = mount.MakeEnvironment()
		env.mounter = m
	}
	return m
}

func (env *environment) Unmounter() mount.Environment {
	u := env.mounter
	if u == nil {
		u = mount.MakeEnvironment()
		env.mounter = u
	}
	return u
}
