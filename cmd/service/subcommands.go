package service

import (
	"errors"

	"github.com/djdv/go-filesystem-utils/cmd/service/control"
	"github.com/djdv/go-filesystem-utils/cmd/service/status"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
)

type (
	// controller implements a `Service`,
	// that can query the service's status
	// and issue control requests;
	// but is not runnable.
	controller struct{}
)

var errControlOnly = errors.New("tried to run service client, not service itself")

func (*controller) Start(service.Service) error { return errControlOnly }
func (*controller) Stop(service.Service) error  { return errControlOnly }

func generateController(request *cmds.Request) (service.Service, error) {
	// NOTE: Do not cache this config.
	// It may contain credentials that may be changed
	// at time of control requests.
	serviceConfig, err := getHostServiceConfig(request)
	if err != nil {
		return nil, err
	}
	// 'La Trahison des Types'
	return service.New((*controller)(nil), serviceConfig)
}

func generateServiceSubcommands() map[string]*cmds.Command {
	const statusName = "status"
	var (
		// controlCommands = generateControlSubcommands()
		controlCommands = control.GenerateCommands(generateController)
		subcommandCount = 1 + // include "status" by default
			len(controlCommands)
		serviceSubs = make(map[string]*cmds.Command, subcommandCount)
	)

	serviceSubs[statusName] = status.GenerateCommand(generateController)
	for i := range controlCommands {
		serviceSubs[service.ControlAction[i]] = controlCommands[i]
	}

	return serviceSubs
}
