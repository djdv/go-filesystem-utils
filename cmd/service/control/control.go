package control

import (
	"errors"
	"fmt"
	"strings"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
)

// controller implements a `Service`,
// that can (only) query the service's status
// and issue control requests;
// it is not runnable.
type controller struct{}

var errControlOnly = errors.New("tried to run service client, not service itself")

func (*controller) Start(service.Service) error { return errControlOnly }
func (*controller) Stop(service.Service) error  { return errControlOnly }

// GenerateCommands provides cmds wrappers
// for each control action provided.
func GenerateCommands(actions ...string) []*cmds.Command {
	controlCommands := make([]*cmds.Command, 0, len(actions))
	for _, controlAction := range actions {
		action := controlAction // NOTE: Value is λ(closed) over below.
		controlCommand := &cmds.Command{
			Helptext: cmds.HelpText{
				Tagline: fmt.Sprintf("%s the service.", strings.Title(controlAction)),
			},
			NoRemote: true,
			Encoders: cmds.Encoders,
			Run: func(request *cmds.Request, _ cmds.ResponseEmitter, env cmds.Environment) (err error) {
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
				serviceConfig, err := fsEnv.Service().Config(request)
				if err != nil {
					return err
				}
				serviceClient, err := service.New((*controller)(nil), serviceConfig)
				if err != nil {
					return err
				}

				// NOTE: We don't currently emit anything here besides errors.
				// (Something like `print("${Control}: Okay")` could be done if desired.)
				//
				// If there's an error it will be returned and encoded|printed.
				// Otherwise output is nothing with exit_code = success.

				return service.Control(serviceClient, action)
			},
		}
		controlCommands = append(controlCommands, controlCommand)
	}
	return controlCommands
}
