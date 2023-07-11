package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/kardianos/service"
	"github.com/multiformats/go-multiaddr"
	"github.com/u-root/uio/ulog"
)

type (
	cleanupFunc     func() error
	serviceSettings struct {
		controlSettings
		daemonWrapper
	}
	serviceOption  func(*serviceSettings) error
	serviceOptions []serviceOption
	daemonWrapper  struct {
		ctx       context.Context
		dbgSysLog service.Logger
		cleanupFn cleanupFunc
		runErrs   <-chan error
		daemonSettings
		maddrSetExplicitly bool
	}
	serviceLog      struct{ service.Logger }
	controlSettings struct {
		service.Config
	}
	controlOption  func(*controlSettings) error
	controlOptions []controlOption
)

const serviceFlagPrefix = "service-"

func Service() command.Command {
	const (
		name     = "service"
		synopsis = "Daemon as a system service."
	)
	usage := header("File system service daemon.") +
		"\n\n" + "Host system services."
	return command.MakeVariadicCommand[serviceOptions](
		name, synopsis, usage, serviceExecute,
		command.WithSubcommands(makeControllerCommands()...),
	)
}

func (so *serviceOptions) BindFlags(flagSet *flag.FlagSet) {
	var (
		daemonOptions  daemonOptions
		controlOptions controlOptions
	)
	bindDaemonFlags(flagSet, &daemonOptions)
	(&controlOptions).BindFlags(flagSet)
	// TODO: can we set this default easily
	// or does cross plat make it annoying?
	// flagSet.Lookup(serverFlagName).
	//	DefValue = userMaddrs[0].String()
	*so = append(*so, func(settings *serviceSettings) error {
		subset := makeDaemonSettings()
		if err := generic.ApplyOptions(&subset, daemonOptions...); err != nil {
			return err
		}
		control, err := controlOptions.make()
		if err != nil {
			return err
		}
		if subset.systemLog == nil {
			subset.systemLog = ulog.Null
		}
		settings.daemonSettings = subset
		settings.controlSettings = control
		return nil
	})
}

func (so serviceOptions) make() (serviceSettings, error) {
	control, err := controlOptions.make(nil)
	if err != nil {
		return serviceSettings{}, err
	}
	settings := serviceSettings{
		controlSettings: control,
	}
	return settings, generic.ApplyOptions(&settings, so...)
}

func serviceExecute(ctx context.Context, options ...serviceOption) error {
	if service.Interactive() {
		return command.UsageError{
			Err: generic.ConstError(
				"this version of the daemon is intended for use by the host service manager" +
					" use the `daemon` command for interactive use",
			),
		}
	}
	settings, err := serviceOptions(options).make()
	if err != nil {
		return err
	}
	svc := &settings.daemonWrapper
	svc.ctx = ctx
	controller, err := service.New(
		svc,
		&settings.Config,
	)
	if err != nil {
		return err
	}
	sysLog, err := controller.SystemLogger(nil)
	if err != nil {
		return err
	}
	svc.daemonSettings.systemLog = serviceLog{sysLog}
	svc.dbgSysLog = sysLog
	// return controller.Run()
	if err := controller.Run(); err != nil {
		sysLog.Error("run:", err)
		return err
	}
	return nil
}

func (svc *daemonWrapper) Start(svcIntf service.Service) error {
	var (
		cleanup  cleanupFunc
		settings = &svc.daemonSettings
	)
	if !svc.maddrSetExplicitly {
		svc.dbgSysLog.Warning("maddrs empty (expected)")
		var (
			serviceMaddrs []multiaddr.Multiaddr
			err           error
		)
		if serviceMaddrs, cleanup, err = createServiceMaddrs(); err != nil {
			return err
		}
		settings.serverMaddrs = serviceMaddrs
		svc.cleanupFn = func() error {
			settings.serverMaddrs = nil
			return cleanup()
		}
	}
	var (
		dCtx, dCancel = context.WithCancel(svc.ctx)
		errs          = make(chan error, 1)
	)
	go func() {
		defer dCancel()
		errs <- daemonRun(dCtx, settings)
	}()
	select {
	default: // Fail-fast check.
	case err := <-errs:
		if cleanup != nil {
			if cErr := cleanup(); cErr != nil {
				err = errors.Join(err, cErr)
			}
		}
		return err
	}
	svc.runErrs = errs
	svc.cleanupFn = cleanup
	return nil
}

func (svc *daemonWrapper) Stop(svcIntf service.Service) error {
	serviceMaddr := svc.serverMaddrs[0]
	if err := shutdownExecute(
		svc.ctx,
		func(settings *shutdownSettings) error {
			settings.serviceMaddr = serviceMaddr
			settings.disposition = immediateShutdown
			return nil
		},
	); err != nil {
		return err
	}
	err := <-svc.runErrs
	if cleanup := svc.cleanupFn; cleanup != nil {
		svc.cleanupFn = nil
		if cErr := cleanup(); cErr != nil {
			err = errors.Join(err, cErr)
		}
	}
	return err
}

func (co *controlOptions) BindFlags(flagSet *flag.FlagSet) {
	const (
		serviceName  = serviceFlagPrefix + "name"
		serviceUsage = "service name associated with service manager"
	)
	flagSetFunc(flagSet, serviceName, serviceUsage, co,
		func(value string, settings *controlSettings) error {
			settings.Config.Name = value
			return nil
		})
	const ( // TODO: we don't need this for run? (only install)
		displayName  = serviceFlagPrefix + "display-name"
		displayUsage = "service display name associate with the service manager"
	)
	flagSetFunc(flagSet, displayName, displayUsage, co,
		func(value string, settings *controlSettings) error {
			settings.Config.DisplayName = value
			return nil
		})
	const (
		descriptionName  = serviceFlagPrefix + "description"
		descriptionUsage = "description to associate with the service manager"
	)
	flagSetFunc(flagSet, descriptionName, descriptionUsage, co,
		func(value string, settings *controlSettings) error {
			settings.Config.Description = value
			return nil
		})
	const (
		userNameName  = serviceFlagPrefix + "username"
		userNameUsage = "username to use when interfacing with the service manager"
	)
	flagSetFunc(flagSet, userNameName, userNameUsage, co,
		func(value string, settings *controlSettings) error {
			settings.Config.UserName = value
			return nil
		})
	const (
		argumentsName  = serviceFlagPrefix + "arguments"
		argumentsUsage = "arguments passed to the service command when started (space separated string)"
	)
	flagSetFunc(flagSet, argumentsName, argumentsUsage, co,
		func(value string, settings *controlSettings) error {
			settings.Config.Arguments = append(
				settings.Config.Arguments,
				splitArgString(value)...,
			)
			return nil
		})
	bindServiceControlFlags(flagSet, co)
}

func splitArgString(input string) []string {
	const (
		quote = '"'
		space = ' '
	)
	var (
		enclosed bool
		head     = 0
		tail     int
		estimate = strings.Count(input, " ")
		list     = make([]string, 0, estimate)
	)
	for i, r := range input {
		tail = i
		switch {
		case r == space && !enclosed:
			list = append(list, input[head:tail])
			head = i + 1
		case r == quote:
			enclosed = !enclosed
		}
	}
	return append(list, input[head:])
}

func (co controlOptions) make() (controlSettings, error) {
	settings := controlSettings{
		Config: service.Config{
			Name:        "go-filesystem",
			DisplayName: "Go File system service",
			Description: "Manages Go file system instances.",
			Arguments: []string{
				"daemon", // TODO: consts
				"service",
			},
		},
	}
	return settings, generic.ApplyOptions(&settings, co...)
}

func makeControllerCommands() []command.Command {
	var (
		actions  = service.ControlAction
		commands = make([]command.Command, len(actions))
	)
	for i, action := range actions {
		var (
			synopsis = fmt.Sprintf(
				"%s the service.", strings.Title(action),
			)
			usage = synopsis
		)
		action := action // Closed over.
		commands[i] = command.MakeVariadicCommand[controlOptions](
			action, synopsis, usage,
			func(_ context.Context, options ...controlOption) error {
				settings, err := controlOptions(options).make()
				if err != nil {
					return err
				}
				serviceClient, err := service.New(
					(service.Interface)(nil),
					&settings.Config,
				)
				if err != nil {
					return err
				}
				return service.Control(serviceClient, action)
			})
	}
	return commands
}

var _ ulog.Logger = (*serviceLog)(nil)

func (sl serviceLog) Printf(format string, v ...any) { sl.Infof(format, v...) }
func (sl serviceLog) Print(v ...any)                 { sl.Info(v...) }
