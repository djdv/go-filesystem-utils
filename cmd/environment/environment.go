// TODO: outdated docstring
// Package environment defines an RPC interface
// that clients may use to interact with the server's environment.
package environment

import (
	"context"

	serviceenv "github.com/djdv/go-filesystem-utils/cmd/service/env"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

func MakeEnvironment(_ context.Context, request *cmds.Request) (cmds.Environment, error) {
	return serviceenv.MakeEnvironment(), nil
}
