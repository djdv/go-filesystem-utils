package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/djdv/go-filesystem-utils/cmd/environment"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/service/host"
	"github.com/djdv/go-filesystem-utils/cmd/service/status"
	fscmds "github.com/djdv/go-filesystem-utils/filesystem/cmds"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
)

const Name = "service"

var Command = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Interact with the host system's service manager",
	},
	NoRemote: true,
	Run:      serviceRun,
	Options:  parameters.CmdsOptionsFrom((*Settings)(nil)),
	Encoders: fscmds.CmdsEncoders,
	Type:     daemon.Response{},
	Subcommands: func() (subCmds map[string]*cmds.Command) {
		const staticCmdsCount = 2
		subCmds = make(map[string]*cmds.Command,
			staticCmdsCount+len(service.ControlAction))
		subCmds[status.Name] = status.Command
		subCmds[daemon.Name] = daemon.Command
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
	serviceEnv, err := environment.Assert(env)
	if err != nil {
		return err
	}

	ctx := request.Context
	settings, err := parseSettings(ctx, request)
	if err != nil {
		return err
	}

	var (
		serviceInterface = &daemonCmdWrapper{
			serviceRequest: request,
			emitter:        emitter,
			environment:    serviceEnv,
		}
		serviceConfig = host.ServiceConfig(&settings.Host)
	)
	serviceController, err := service.New(serviceInterface, serviceConfig)
	if err != nil {
		return err
	}

	sysLog, err := serviceController.SystemLogger(nil)
	if err != nil {
		return err
	}

	maddrsProvided := len(settings.ServiceMaddrs) > 0
	serviceListeners, cleanup, err := systemListeners(maddrsProvided, sysLog)
	if err != nil {
		err = fmt.Errorf("sysListener returned err: %w", err)
		return err
	}
	if serviceListeners != nil {
		daemon.UseListeners(serviceListeners...)
	}

	serviceInterface.sysLog = sysLog
	serviceInterface.cleanup = cleanup

	return serviceController.Run()
}

type (
	daemonCmdWrapper struct {
		serviceRequest *cmds.Request
		emitter        cmds.ResponseEmitter
		environment    environment.Environment

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
	)

	// Daemon started successfully.
	// Handle any post-init messages from the daemon in the background.
	runErrs := make(chan error)
	go func() {
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
			// Ask the system service manager to send our process its stop signal.
			// (`service.Service.Stop`)
			// This will unblock `service.Service.Run`, which will then call
			// `service.Interface.Stop` (our `Stop` implementation)
			//
			// NOTE: If the system refuses our request to Stop
			// there's nothing we can do about it other than try to log it.
			// The system operator will have to stop us manually via the host itself.
			// (`Stop-Service`, `service stop`, `svcadm disable`, `launchctl unload`, etc.)
			if svcErr := svcIntf.Stop(); svcErr != nil {
				logErr(sysLog, svcErr)
			}
		}
	}()

	// Store the error channel so that `Stop`
	// may return any errors encountered during runtime.
	svc.runErrs = runErrs

	return nil
}

func (svc *daemonCmdWrapper) Stop(svcIntf service.Service) (err error) {
	var (
		runErrs = svc.runErrs
		sysLog  = svc.sysLog
		cleanup = svc.cleanup
	)
	if runErrs == nil {
		return logErr(sysLog, errors.New("service wasn't started"))
	}
	svc.runErrs = nil
	svc.cleanup = nil

	defer func() { // NOTE: Read+Writes to named return value.
		err = cleanupAndLog(sysLog, cleanup, err)
	}()

	stopper := svc.environment.Daemon().Stopper()
	select {
	// If an error was encountered during startup|runtime;
	// env's Stop method should/will be called
	// and this case will not block.
	case runErr, ok := <-runErrs:
		if !ok {
			// Daemon stopped gracefully already.
			// Returning here stops the system service.
			return nil
		}
		// Buffer the first error we got,
		// there may be more coming from the channel.
		// err = logErr(sysLog, runErr)
		err = runErr

	// Otherwise, we'll call env's Stop method ourself
	// which will indirectly unblock runErr.
	default:
		if stopErr := stopper.Stop(environment.Requested); stopErr != nil {
			err = stopErr
			return
		}
	}

	// Wait for daemon's Run method to fully stop / return all values.
	for runErr := range runErrs {
		runErr = logErr(sysLog, runErr)
		if err == nil {
			err = runErr
		} else {
			err = fmt.Errorf("%w - %s", err, runErr)
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
