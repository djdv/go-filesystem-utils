package environment

type (
	Daemon interface {
		Stopper() Stopper
	}
	daemon struct {
		stopper Stopper
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
