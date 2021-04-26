package status

import (
	"errors"
	"fmt"
	"io"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
	"github.com/multiformats/go-multiaddr"
)

// ServiceClientFunc should parses service options in the request,
// and return a service interface which accepts status requests.
type ServiceClientFunc func(request *cmds.Request) (service.Service, error)

// GenerateCommands generates a cmd which
// uses the service controller from getClient
// to query the daemon and service status.
func GenerateCommand(getClient ServiceClientFunc) *cmds.Command {
	return &cmds.Command{
		Helptext: cmds.HelpText{
			Tagline: "Retrieve the current service status.",
		},
		NoRemote: true,
		Encoders: cmds.Encoders,
		Type:     Status{},
		Run: func(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) (err error) {
			serviceClient, err := getClient(request)
			if err != nil {
				return err
			}

			var statusResponse Status
			controllerStatus, svcErr := serviceClient.Status()
			if svcErr != nil {
				statusResponse.ControllerError = svcErr
			} else {
				statusResponse.ControllerStatus = controllerStatus
			}

			serviceMaddr, _, err := fscmds.GetServiceMaddr(request)
			if err != nil {
				if !errors.Is(err, fscmds.ErrServiceNotFound) {
					return err
				}
			} else {
				if fscmds.ClientDialable(serviceMaddr) {
					statusResponse.DaemonListener = serviceMaddr
				}
			}

			return emitter.Emit(&statusResponse)
		},
		PostRun: cmds.PostRunMap{
			cmds.CLI: formatStatus,
		},
	}
}

// TODO: Text encoder.
type Status struct {
	DaemonListener   multiaddr.Multiaddr
	ControllerStatus service.Status
	ControllerError  error
}

func formatStatus(response cmds.Response, emitter cmds.ResponseEmitter) error {
	outputs := formats.MakeOptionalOutputs(response.Request(), emitter)
	for {
		untypedResponse, err := response.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = nil
			}
			return err
		}

		responseValue, ok := untypedResponse.(*Status)
		if !ok {
			return cmds.Errorf(cmds.ErrImplementation,
				"emitter sent unexpected type+value: %#v", untypedResponse)
		}

		if err := outputs.Emit(responseValue); err != nil {
			return err
		}

		var daemonStatus string
		if responseValue.DaemonListener == nil {
			daemonStatus = "Daemon: Not running.\n"
		} else {
			daemonStatus = fmt.Sprintf("Daemon listening on: %s\n",
				responseValue.DaemonListener)
		}
		if err = outputs.Print(daemonStatus); err != nil {
			return err
		}

		var (
			svcErr              = responseValue.ControllerError
			serviceNotInstalled = svcErr != nil &&
				errors.Is(svcErr, service.ErrNotInstalled)
		)
		if svcErr != nil && !serviceNotInstalled {
			controllerMessage := fmt.Sprintf("Service controller: %s\n",
				svcErr)
			if err = outputs.Print(controllerMessage); err != nil {
				return err
			}
		}

		if err := outputs.Print("System service status: "); err != nil {
			return err
		}
		var serviceStatus string
		switch responseValue.ControllerStatus {
		case service.StatusRunning:
			serviceStatus = "Running.\n"
		case service.StatusStopped:
			serviceStatus = "Stopped.\n"
		default:
			if serviceNotInstalled {
				serviceStatus = "Not Installed.\n"
			} else {
				serviceStatus = "Unknown.\n"
			}
		}
		if err := outputs.Print(serviceStatus); err != nil {
			return err
		}
	}
}
