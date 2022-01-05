package environment

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
		m = new(mounter)
		env.mounter = m
	}
	return m
}

func (env *daemon) Unmounter() Mounter { // TODO: separate mount/unmount interfaces
	u := env.mounter
	if u == nil {
		u = new(mounter)
		env.mounter = u
	}
	return u
}
