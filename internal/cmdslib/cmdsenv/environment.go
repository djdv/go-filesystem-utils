// TODO: outdated docstring
// Package environment defines an RPC interface
// that clients may use to interact with the server's environment.
package cmdsenv

import (
	"context"
	"reflect"

	cmds "github.com/ipfs/go-ipfs-cmds"
)

type (
	Environment interface {
		Daemon() Daemon
	}
	cmdsEnvironment struct {
		*daemon
	}
)

func (env *cmdsEnvironment) Daemon() Daemon {
	service := env.daemon
	if service == nil {
		service = new(daemon)
		service.mounter.Context = context.TODO()
		env.daemon = service
	}
	return service
}

func MakeEnvironment(_ context.Context, request *cmds.Request) (cmds.Environment, error) {
	return new(cmdsEnvironment), nil
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