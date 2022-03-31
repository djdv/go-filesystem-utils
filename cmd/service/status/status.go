package status

import (
	"errors"
	"strings"

	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/service/host"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
	"github.com/multiformats/go-multiaddr"
)

const Name = "status"

type (
	Response struct {
		SystemController
		Listeners []multiaddr.Multiaddr
	}
	SystemController struct {
		Error error
		service.Status
	}
)

func (r *Response) String() string {
	var output strings.Builder
	output.WriteString("Daemon:")
	listeners := r.Listeners
	if listeners == nil {
		output.WriteString("\n\tNo listeners")
	}
	for _, listenerMaddr := range listeners {
		output.WriteString("\n\tListening on: " + listenerMaddr.String())
	}
	output.WriteRune('\n')

	output.WriteString(r.SystemController.String())
	return output.String()
}

func (sc *SystemController) String() string {
	var (
		output              strings.Builder
		err                 = sc.Error
		serviceNotInstalled = err != nil &&
			errors.Is(err, service.ErrNotInstalled)
	)
	status, knownCode := map[service.Status]string{
		service.StatusRunning: "Running",
		service.StatusStopped: "Stopped",
	}[sc.Status]
	if !knownCode {
		if serviceNotInstalled {
			status = "Not installed"
		} else {
			status = "Unknown"
		}
	}
	output.WriteString(
		"Service controller:" +
			"\n\tStatus: " + status,
	)
	if err != nil && !serviceNotInstalled {
		output.WriteString("\n\tError: " + err.Error())
	}

	output.WriteRune('\n')
	return output.String()
}

// Command status queries the status of the service daemon
// and the operating system's own service manager.
var Command = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Retrieve the current service status.",
	},
	NoRemote: true,
	Encoders: cmds.Encoders,
	Type:     Response{},
	Run: func(request *cmds.Request, emitter cmds.ResponseEmitter, _ cmds.Environment) error {
		ctx := request.Context
		statusSettings, err := settings.ParseAll[Settings](ctx, request)
		if err != nil {
			return err
		}
		serviceConfig := host.ServiceConfig(&statusSettings.Host)

		// Query the host system service manager.
		serviceClient, err := service.New((service.Interface)(nil), serviceConfig)
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
		serviceMaddrs := statusSettings.ServiceMaddrs
		if len(serviceMaddrs) == 0 {
			if serviceMaddrs, err = defaultMaddrs(); err != nil {
				return err
			}
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
			// There's no point in sending+process an empty slice, versus not sending nil at all.
			response.Listeners = listeners
		}

		return emitter.Emit(&response)
	},
}

func defaultMaddrs() ([]multiaddr.Multiaddr, error) {
	userMaddrs, err := daemon.UserServiceMaddrs()
	if err != nil {
		return nil, err
	}
	systemMaddrs, err := daemon.SystemServiceMaddrs()
	if err != nil {
		return nil, err
	}
	return append(userMaddrs, systemMaddrs...), nil
}
