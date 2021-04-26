package service

import (
	"errors"

	"github.com/djdv/go-filesystem-utils/cmd/parameters"
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
	var (
		ctx             = request.Context
		settings        = new(Settings)
		unsetArgs, errs = parameters.ParseSettings(ctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
	)
	if _, err := parameters.AccumulateArgs(ctx, unsetArgs, errs); err != nil {
		return nil, err
	}
	serviceConfig := &service.Config{
		Name:        serviceName,
		DisplayName: serviceDisplayName,
		Description: description,
		UserName:    settings.Username,
		// NOTE: field  names and type in the platform struct,
		// must match the map keys defined in the `service.KeyValue` pkg documentation.
		Option:    serviceKeyValueFrom(&settings.PlatformSettings),
		Arguments: serviceArgs(settings),
	}
	return service.New((*controller)(nil), serviceConfig)
}

func generateServiceSubcommands() map[string]*cmds.Command {
	const statusName = "status"
	var (
		statusCommand   = status.GenerateCommand(generateController)
		controlCommands = control.GenerateCommands(generateController)
		subcommandCount = len(controlCommands) +
			1 // Count the "status" subcommand.
		serviceSubs = make(map[string]*cmds.Command, subcommandCount)
	)

	for i := range controlCommands {
		serviceSubs[service.ControlAction[i]] = controlCommands[i]
	}
	serviceSubs[statusName] = statusCommand

	return serviceSubs
}
