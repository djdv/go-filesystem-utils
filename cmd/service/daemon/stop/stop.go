package stop

import (
	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	serviceenv "github.com/djdv/go-filesystem-utils/cmd/service/env"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop/env"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

const Name = "stop"

// Command `stop` requests that the running service instance ceases operations.
var Command = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Stop the currently running service instance.",
	},
	NoLocal:  true,
	Encoders: formats.CmdsEncoders,
	Run: func(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
		var (
			ctx             = request.Context
			settings        = new(fscmds.Settings)
			unsetArgs, errs = parameters.ParseSettings(ctx, settings,
				parameters.SettingsFromCmds(request),
				parameters.SettingsFromEnvironment(),
			)
		)
		if _, err := parameters.AccumulateArgs(ctx, unsetArgs, errs); err != nil {
			return err
		}

		serviceEnv, err := serviceenv.Assert(env)
		if err != nil {
			return err
		}

		if err := serviceEnv.Daemon().Stopper().Stop(stop.Requested); err != nil {
			return err
		}

		return nil
	},
}
