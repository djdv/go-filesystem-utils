package status

import (
	"errors"
	"fmt"
	"io"
	"strings"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
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
		Run: func(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
			serviceClient, err := getClient(request)
			if err != nil {
				return err
			}

			var (
				ctx             = request.Context
				settings        = new(fscmds.Settings)
				unsetArgs, errs = parameters.ParseSettings(ctx, settings,
					parameters.SettingsFromCmds(request),
					parameters.SettingsFromEnvironment(),
				)

				statusResponse Status
			)
			if _, err := parameters.AccumulateArgs(ctx, unsetArgs, errs); err != nil {
				return err
			}

			if controllerStatus, svcErr := serviceClient.Status(); svcErr != nil {
				statusResponse.ControllerError = svcErr
			} else {
				statusResponse.ControllerStatus = controllerStatus
			}

			serviceMaddrs := settings.ServiceMaddrs
			if len(serviceMaddrs) == 0 {
				userMaddrs, err := fscmds.UserServiceMaddrs()
				if err != nil {
					return err
				}
				systemMaddrs, err := fscmds.SystemServiceMaddrs()
				if err != nil {
					return err
				}
				serviceMaddrs = append(userMaddrs, systemMaddrs...)
			}

			for _, serviceMaddr := range serviceMaddrs {
				if fscmds.ServerDialable(serviceMaddr) {
					statusResponse.DaemonListeners = append(
						statusResponse.DaemonListeners,
						serviceMaddr,
					)
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
	DaemonListeners  []multiaddr.Multiaddr
	ControllerStatus service.Status
	ControllerError  error
}

func statusString(stat service.Status) string {
	if status, ok := map[service.Status]string{
		service.StatusRunning: "Running",
		service.StatusStopped: "Stopped",
	}[stat]; ok {
		return status
	}
	return "Unknown"
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
		if responseValue.DaemonListeners == nil {
			daemonStatus = "Daemon: Not running.\n"
		} else {
			// TODO: tabularize instead
			var sb strings.Builder
			sb.WriteString("Daemon listening on:")
			for _, listenerMaddr := range responseValue.DaemonListeners {
				sb.WriteString("\n\t" + listenerMaddr.String())
			}
			sb.WriteString("\n")
			daemonStatus = sb.String()
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
		if serviceNotInstalled {
			serviceStatus = "Not Installed.\n"
		} else {
			serviceStatus = statusString(responseValue.ControllerStatus) + ".\n"
		}

		if err := outputs.Print(serviceStatus); err != nil {
			return err
		}
	}
}
