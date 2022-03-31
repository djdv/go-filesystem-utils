package cmdsenv

type (
	Daemon interface {
		Stopper() Stopper
		Mounter
	}
	daemon struct {
		stopper Stopper
		mounter
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
