package environment

import (
	"context"
)

type (
	Daemon interface {
		Stopper() Stopper
		Lister() Index
		Mounter() Mounter
	}
	daemon struct {
		stopper Stopper
		lister  Index
		mounter Mounter
	}
)

func (env *daemon) Stopper() Stopper {
	s := env.stopper
	if s == nil {
		s = new(stopper)
		env.stopper = s
	}
	return s
}

func (env *daemon) Lister() Index {
	l := env.lister
	if l == nil {
		l = new(index)
		env.lister = l
	}
	return l
}

func (env *daemon) Mounter() Mounter {
	m := env.mounter
	if m == nil {
		m = &mounter{Context: context.TODO()}
		env.mounter = m
	}
	return m
}

func (env *daemon) Unmounter() Mounter { // TODO: separate mount/unmount interfaces
	u := env.mounter
	if u == nil {
		u = &mounter{Context: context.TODO()}
		env.mounter = u
	}
	return u
}
