package control

import (
	"fmt"
	"strings"

	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
)

// ServiceClientFunc is expected to parse service options in the request,
// and return a service interface.
// The returned interface is expected to accept service controller requests,
// but not be runnable by itself.
type ServiceClientFunc func(request *cmds.Request) (service.Service, error)

// GenerateCommands provides cmds wrappers
// for each service.ControlAction available.
// Using the service controller from getClient
// to communicate with the host service manager.
func GenerateCommands(getClient ServiceClientFunc) []*cmds.Command {
	controlCommands := make([]*cmds.Command, 0, len(service.ControlAction))
	for _, controlAction := range service.ControlAction {
		// NOTE: Copy the range value.
		// (It's closed over in Run below)
		action := controlAction
		controlCommand := &cmds.Command{
			Helptext: cmds.HelpText{
				Tagline: fmt.Sprintf("%s the service.", strings.Title(controlAction)),
			},
			NoRemote: true,
			Encoders: cmds.Encoders,
			Run: func(request *cmds.Request, _ cmds.ResponseEmitter, _ cmds.Environment) (err error) {
				serviceClient, err := getClient(request)
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
