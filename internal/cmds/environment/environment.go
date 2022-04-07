// TODO: outdated docstring
// Package environment defines an RPC interface
// that clients may use to interact with the server's environment.
package cmdsenv

import (
	"context"
	"reflect"

	"github.com/djdv/go-filesystem-utils/internal/cmds/environment/daemon"
	"github.com/djdv/go-filesystem-utils/internal/cmds/environment/stop"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

type (
	Environment interface {
		Context() context.Context
		Stopper() stop.Stopper
		Daemon() daemon.Daemon
	}
	cmdsEnvironment struct {
		context context.Context
		stopper
		daemon daemon.Daemon
	}
)

func (env *cmdsEnvironment) Context() context.Context { return env.context }

func (env *cmdsEnvironment) Stopper() stop.Stopper { return &env.stopper }

func (env *cmdsEnvironment) Daemon() daemon.Daemon {
	service := env.daemon
	if service == nil {
		service = daemon.New(env.context)
		env.daemon = service
	}
	return service
}

func makeEnvironment(ctx context.Context, request *cmds.Request) *cmdsEnvironment {
	return &cmdsEnvironment{context: ctx}
}

func MakeEnvironment(ctx context.Context, request *cmds.Request) (cmds.Environment, error) {
	// TODO: only return env for commands that need it, otherwise return nil.
	// use common lookup helper function (we'll need it in exec too)
	// pkg.isCommand(request, pkg.Daemon, pkg.Service, ...)
	return makeEnvironment(ctx, request), nil
}

func Assert(environment cmds.Environment) (Environment, error) {
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
