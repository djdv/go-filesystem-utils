package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/service/host"
	"github.com/djdv/go-filesystem-utils/cmd/service/status"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/cmdsenv"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings/options"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
	manet "github.com/multiformats/go-multiaddr/net"
)

const Name = "service"

var Command = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Interact with the host system's service manager",
	},
	NoRemote: true,
	Run:      serviceRun,
	Options:  options.MustMakeCmdsOptions[Settings](),
	Encoders: settings.CmdsEncoders,
	Type:     daemon.Response{},
	Subcommands: func() (subCmds map[string]*cmds.Command) {
		const staticCmdsCount = 2
		subCmds = make(map[string]*cmds.Command,
			staticCmdsCount+len(service.ControlAction))
		subCmds[status.Name] = status.Command
		subCmds[daemon.Name] = daemon.Command()
		registerControllerCommands(subCmds, service.ControlAction[:]...)
		return
	}(),
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
	serviceSettings, err := settings.ParseAll[Settings](ctx, request)
	if err != nil {
		return err
	}

	var (
		serviceInterface = &daemonCmdWrapper{
			serviceRequest: request,
			emitter:        emitter,
			environment:    serviceEnv,
		}
		serviceConfig = host.ServiceConfig(&serviceSettings.Host)
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

type (
	daemonCmdWrapper struct {
		serviceRequest *cmds.Request
		emitter        cmds.ResponseEmitter
		environment    cmdsenv.Environment
		hostListeners  []manet.Listener

		sysLog  service.Logger
		runErrs <-chan error
		cleanup cleanupFunc
	}
)

// logErr logs and returns the passed in error,
// along with a (wrapped) error from the logger itself if encountered.
func logErr(logger service.Logger, err error) error {
	if logErr := logger.Error(err); logErr != nil {
		return fmt.Errorf("%w - %s", err, logErr)
	}
	return err
}

func (svc *daemonCmdWrapper) Start(svcIntf service.Service) error {
	if svc.runErrs != nil {
		return errors.New("service already started")
	}

	var (
		sysLog         = svc.sysLog
		serviceRequest = svc.serviceRequest
		ctx            = serviceRequest.Context
	)
	daemonRequest, err := cmds.NewRequest(ctx, daemon.CmdsPath(),
		serviceRequest.Options, nil, nil, serviceRequest.Root)
	if err != nil {
		return logErr(sysLog, err)
	}
	if listeners := svc.hostListeners; listeners != nil {
		if err := daemon.UseHostListeners(daemonRequest, listeners); err != nil {
			return logErr(sysLog, err)
		}
	}

	// The daemon command should block until the daemon stops listening
	// or encounters an error. Emitting responses while it runs.
	daemonEmitter, daemonResponse := cmds.NewChanResponsePair(daemonRequest)
	go serviceRequest.Root.Call(daemonRequest, daemonEmitter, svc.environment)

	var (
		startupHandler = func(response *daemon.Response) error {
			return sysLog.Info(response.String())
		}

		sawStopResponse bool
		runtimeHandler  = func(response *daemon.Response) error {
			if response.Status == daemon.Stopping {
				sawStopResponse = true
			}
			return sysLog.Info(response.String())
		}

		startup, runtime = daemon.SplitResponse(daemonResponse, startupHandler, runtimeHandler)
		runErrs          = make(chan error)
		handleResponses  = func() {
			defer close(runErrs)
			for _, f := range []func() error{
				startup,
				runtime,
			} {
				if err := f(); err != nil {
					runErrs <- err
				}
			}
			if !sawStopResponse {
				// NOTE: If the Stop request fails, there's nothing we can do.
				// The system operator will have to stop the service manually.
				if svcErr := svcIntf.Stop(); svcErr != nil {
					_ = logErr(sysLog, svcErr)
				}
			}
		}
	)

	svc.runErrs = runErrs
	go handleResponses()

	return nil
}

func (svc *daemonCmdWrapper) Stop(svcIntf service.Service) (err error) {
	var (
		daemonErrs = svc.runErrs
		sysLog     = svc.sysLog
		cleanup    = svc.cleanup
	)
	if daemonErrs == nil {
		return logErr(sysLog, errors.New("service wasn't started"))
	}
	svc.runErrs = nil
	svc.cleanup = nil

	defer func() { // NOTE: Read+Writes to named return value.
		err = cleanupAndLog(sysLog, cleanup, err)
	}()

	stopper := svc.environment.Daemon().Stopper()
	select {
	case daemonErr, running := <-daemonErrs:
		if !running {
			return nil
		}
		err = daemonErr
	default:
		if stopErr := stopper.Stop(cmdsenv.Requested); stopErr != nil {
			err = stopErr
			return
		}
	}

	for daemonErr := range daemonErrs {
		daemonErr = logErr(sysLog, daemonErr)
		if err == nil {
			err = daemonErr
		} else {
			err = fmt.Errorf("%w - %s", err, daemonErr)
		}
	}
	return err
}

type cleanupFunc func() error

func cleanupAndLog(sysLog service.Logger, cleanup cleanupFunc, err error) error {
	err = errWithCleanup(err, cleanup)
	if err != nil {
		err = logErr(sysLog, err)
	}
	return err
}

func errWithCleanup(err error, cleanup cleanupFunc) error {
	if cleanup == nil {
		return err
	}
	if cleanupErr := cleanup(); cleanupErr != nil {
		if err == nil {
			return cleanupErr
		}
		return fmt.Errorf("%w - %s", err, cleanupErr)
	}
	return err
}
