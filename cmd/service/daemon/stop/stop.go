package stop

import (
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/cmdsenv"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

const Name = "stop"

// Command `stop` requests that the running service instance ceases operations.
var Command = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Stop the currently running service instance.",
	},
	NoLocal:  true,
	Encoders: settings.CmdsEncoders,
	Run: func(request *cmds.Request, _ cmds.ResponseEmitter, env cmds.Environment) error {
		/*
			ctx := request.Context
				stopSettings, err := settings.ParseAll[settings.Settings](ctx, request)
				if err != nil {
					return err
				}
		*/

		serviceEnv, err := cmdsenv.Assert(env)
		if err != nil {
			return err
		}

		if err := serviceEnv.Daemon().Stopper().Stop(cmdsenv.Requested); err != nil {
			return err
		}

		return nil
	},
}
