package stop

import (
	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

const Name = "stop"

// Stop requests that the running service instance ceases operations.
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

		fsEnv, err := environment.CastEnvironment(env)
		if err != nil {
			return err
		}

		// TODO: format this error? Probably not.
		return fsEnv.Daemon().Stop(daemon.StopRequested)
	},
}
