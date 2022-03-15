// TODO: outdated docstring
// Package environment defines an RPC interface
// that clients may use to interact with the server's environment.
package environment

import (
	"context"
	"reflect"

	cmds "github.com/ipfs/go-ipfs-cmds"
)

type (
	Environment interface{}
	environment struct{}
)

func MakeEnvironment(_ context.Context, request *cmds.Request) (cmds.Environment, error) {
	return new(environment), nil
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
