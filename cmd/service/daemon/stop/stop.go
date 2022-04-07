package stop

import (
	cmdsenv "github.com/djdv/go-filesystem-utils/internal/cmds/environment"
	"github.com/djdv/go-filesystem-utils/internal/cmds/environment/stop"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

const Name = "stop"

// Command `stop` requests that the running service instance ceases operations.
var Command = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Stop the currently running service instance.",
	},
	NoLocal:  true,
	Encoders: settings.Encoders,
	Run: func(_ *cmds.Request, _ cmds.ResponseEmitter, env cmds.Environment) error {
		serviceEnv, err := cmdsenv.Assert(env)
		if err != nil {
			return err
		}
		if err := serviceEnv.Stopper().Stop(stop.Requested); err != nil {
			return err
		}
		return nil
	},
}
