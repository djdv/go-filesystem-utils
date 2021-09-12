package environment

import (
	"context"
	"reflect"

	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment/service"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

type (
	// TODO: Consider.
	// Let the Env interface simply be a constructor for sub-interfaces
	// This seems better, but will require some refactoring.
	Environment interface {
		Context() context.Context
		Service() service.Service
		Daemon() daemon.Daemon
	}

	filesystemEnvironment struct {
		// Mutex should be used and respected for
		// operations that may be called from multiple processes.
		//sync.Mutex
		// Every environment context that can be
		// derived or canceled, should be derived or canceled
		// from this context.
		rootCtx context.Context
		daemon  daemon.Daemon
	}
)

// TODO: move to _test.go
//var _ environment.Environment = (*filesystemEnvironment)(nil)

func (env *filesystemEnvironment) Service() service.Service { return service.NewServiceEnvironment() }
func (fe *filesystemEnvironment) Daemon() daemon.Daemon {
	de := fe.daemon
	if de == nil {
		de = daemon.NewDaemonEnvironment()
		fe.daemon = de
	}

	return de
}

func (fe *filesystemEnvironment) Context() context.Context { return fe.rootCtx }

func MakeEnvironment(ctx context.Context, request *cmds.Request) (cmds.Environment, error) {
	env := &filesystemEnvironment{
		rootCtx: ctx,
	}
	return env, nil
}

func CastEnvironment(environment cmds.Environment) (Environment, error) {
	typedEnv, isUsable := environment.(Environment)
	if !isUsable {
		interfaceType := reflect.TypeOf((*Environment)(nil)).Elem()
		interfaceName := interfaceType.PkgPath() + "." + interfaceType.Name()
		return nil, cmds.Errorf(cmds.ErrImplementation,
			"%T does not implement the %s interface",
			environment, interfaceName,
		)
	}
	return typedEnv, nil
}
