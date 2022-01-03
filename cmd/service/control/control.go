package control

import (
	"errors"
	"fmt"
	"strings"

	"github.com/djdv/go-filesystem-utils/cmd/service/host"
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

// TODO: just return this as a map, no args either.
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
				ctx := request.Context
				settings, err := parseSettings(ctx, request)
				if err != nil {
					return err
				}
				serviceConfig := host.ServiceConfig(&settings.Host)
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
