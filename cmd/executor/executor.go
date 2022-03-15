package executor

import (
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// MakeExecutor constructs a cmds-lib executor; which parses the Request and
// determines whether to execute the Command within a local or remote process.
//
// If no remote addresses are provided in the request,
// a local service instance will be created and used automatically.
func MakeExecutor(request *cmds.Request, env interface{}) (cmds.Executor, error) {
	// Execute the request locally if we can.
	if request.Command.NoRemote ||
		!request.Command.NoLocal {
		return cmds.NewExecutor(request.Root), nil
	}
	return nil, cmds.Errorf(cmds.ErrClient, "remote commands not implemented in this build")
}
