package status

import (
	"errors"

	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/service/host"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
	"github.com/multiformats/go-multiaddr"
)

// controller implements a `Service`,
// that can (only) query the service's status
// and issue control requests;
// it is not runnable.
type controller struct{}

var errControlOnly = errors.New("tried to run service client, not service itself")

func (*controller) Start(service.Service) error { return errControlOnly }
func (*controller) Stop(service.Service) error  { return errControlOnly }

const Name = "status"

type (
	SystemController struct {
		Error error
		service.Status
	}
	Response struct {
		SystemController
		Listeners []multiaddr.Multiaddr
	}
)

// Status queries the status of the service daemon
// and the operating system's own service manager.
var Command = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Retrieve the current service status.",
	},
	NoRemote: true,
	Encoders: cmds.Encoders,
	Type:     Response{},
	Run: func(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
		ctx := request.Context

		settings, err := parseSettings(ctx, request)
		if err != nil {
			return err
		}
		serviceConfig := host.ServiceConfig(&settings.host)

		// Query the host system service manager.
		serviceClient, err := service.New((*controller)(nil), serviceConfig)
		if err != nil {
			return err
		}

		var response Response
		controllerStatus, svcErr := serviceClient.Status()
		response.SystemController = SystemController{
			Status: controllerStatus,
			Error:  svcErr,
		}

		// Query host system service servers.
		serviceMaddrs := settings.ServiceMaddrs
		if len(serviceMaddrs) == 0 {
			userMaddrs, err := daemon.UserServiceMaddrs()
			if err != nil {
				return err
			}
			systemMaddrs, err := daemon.SystemServiceMaddrs()
			if err != nil {
				return err
			}
			serviceMaddrs = append(userMaddrs, systemMaddrs...)
		}

		listeners := make([]multiaddr.Multiaddr, 0, len(serviceMaddrs))
		for _, serviceMaddr := range serviceMaddrs {
			if daemon.ServerDialable(serviceMaddr) {
				listeners = append(
					listeners,
					serviceMaddr,
				)
			}
		}
		if len(listeners) != 0 {
			// NOTE: This only matters because we're encoding and emitting this struct.
			// There's no point in processing an empty list versus nil.
			response.Listeners = listeners
		}

		return emitter.Emit(&response)
	},
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

/*
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

		responseValue, ok := untypedResponse.(*Response)
		if !ok {
			return cmds.Errorf(cmds.ErrImplementation,
				"emitter sent unexpected type+value: %#v", untypedResponse)
		}

		if err := outputs.Emit(responseValue); err != nil {
			return err
		}

		var (
			daemonStatus    string
			daemonListeners = responseValue.Listeners
		)
		if len(daemonListeners) == 0 {
			daemonStatus = "Daemon: Not running.\n"
		} else {
			// TODO: tabularize instead
			var sb strings.Builder
			sb.WriteString("Daemon listening on:")
			for _, listenerMaddr := range daemonListeners {
				sb.WriteString("\n\t" + listenerMaddr.String())
			}
			sb.WriteString("\n")
			daemonStatus = sb.String()
		}
		if err = outputs.Print(daemonStatus); err != nil {
			return err
		}

		var (
			svcErr              = responseValue.SystemController.Error
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
			serviceStatus = statusString(responseValue.SystemController.Status) + ".\n"
		}

		if err := outputs.Print(serviceStatus); err != nil {
			return err
		}
	}
}
*/
