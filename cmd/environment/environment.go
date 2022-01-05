// TODO: outdated docstring
// Package environment defines an RPC interface
// that clients may use to interact with the server's environment.
package environment

import (
	"context"
	"reflect"

	daemon "github.com/djdv/go-filesystem-utils/cmd/service/daemon/env"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

type (
	Environment interface {
		Daemon() daemon.Environment
	}
	environment struct {
		daemon daemon.Environment
	}
)

var _ Environment = (*environment)(nil)

func MakeEnvironment(_ context.Context, request *cmds.Request) (cmds.Environment, error) {
	return &environment{}, nil
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

func (env *environment) Daemon() daemon.Environment {
	d := env.daemon
	if d == nil {
		d = daemon.MakeEnvironment()
		env.daemon = d
	}
	return d
}
