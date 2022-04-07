package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	"github.com/djdv/go-filesystem-utils/internal/cmds/environment"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
)

const (
	Name       = "service"
	StatusName = "status"
)

type Settings struct {
	Username string `settings:"arguments"`
	ServiceName,
	ServiceDisplayName,
	ServiceDescription string
	PlatformSettings

	settings.Root
}

func (*Settings) Parameters(ctx context.Context) parameters.Parameters {
	partialParams := []runtime.CmdsParameter{
		{HelpText: "Username to use when interfacing with the system service manager."},
		{HelpText: "Service name (usually as a command argument) to associate with the service (when installing)"},
		{HelpText: "Service display name (usually seen in UI labels) to associate with the service (when installing)"},
		{HelpText: "Description (usually seen in UI labels) to associate with the service (when installing)"},
	}
	return CtxJoin(ctx,
		runtime.GenerateParameters[Settings](ctx, partialParams),
		(*PlatformSettings).Parameters(nil, ctx),
		(*settings.Root).Parameters(nil, ctx),
	)
}

func Command() *cmds.Command {
	return &cmds.Command{
		Helptext: cmds.HelpText{
			Tagline: "Interact with the host system's service manager",
		},
		NoRemote: true,
		Run:      serviceRun,
		Options: append(
			settings.MakeOptions[Settings](),
			settings.MakeOptions[PlatformSettings]()...,
		),
		Encoders: settings.CmdsEncoders,
		Type:     daemon.Response{},
		Subcommands: func() (subCmds map[string]*cmds.Command) {
			const staticCmdsCount = 2
			subCmds = make(map[string]*cmds.Command,
				staticCmdsCount+len(service.ControlAction))
			subCmds[StatusName] = StatusCommand()
			subCmds[daemon.Name] = daemon.Command()
			registerControllerCommands(subCmds, service.ControlAction[:]...)
			return
		}(),
	}
}

func serviceRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	if service.Interactive() {
		return fmt.Errorf(
			"This version of the daemon is intended for use by the host service manager."+
				" Use `%s` for interactive use.",
			strings.Join(append(request.Path, daemon.Name), " "),
		)
	}

	// NOTE: We don't have the system logger yet.
	// Early errors will only show up in tests or a debugger.
	serviceEnv, err := cmdsenv.Assert(env)
	if err != nil {
		return err
	}

	ctx := request.Context
	serviceSettings, err := settings.Parse[Settings](ctx, request)
	if err != nil {
		return err
	}

	var (
		serviceInterface = &daemonCmdWrapper{
			serviceRequest: request,
			emitter:        emitter,
			environment:    serviceEnv,
		}
		serviceConfig = serviceConfig(serviceSettings)
	)
	serviceController, err := service.New(serviceInterface, serviceConfig)
	if err != nil {
		return err
	}

	sysLog, err := serviceController.SystemLogger(nil)
	if err != nil {
		return err
	}

	maddrsProvided := len(serviceSettings.ServiceMaddrs) > 0
	serviceListeners, cleanup, err := systemListeners(maddrsProvided, sysLog)
	if err != nil {
		return err
	}
	if serviceListeners != nil {
		serviceInterface.hostListeners = serviceListeners
	}

	serviceInterface.sysLog = sysLog
	serviceInterface.cleanup = cleanup

	return serviceController.Run()
}
