package daemon

import (
	"github.com/djdv/go-filesystem-utils/cmd/environment/service/daemon/stop"
)

type (
	Environment interface {
		Stopper() stop.Environment
	}
	environment struct {
		stopper stop.Environment
	}
)

func (env *environment) Stopper() stop.Environment {
	s := env.stopper
	if s == nil {
		s = stop.MakeEnvironment()
		env.stopper = s
	}
	return s
}

func MakeEnvironment() Environment { return &environment{} }
