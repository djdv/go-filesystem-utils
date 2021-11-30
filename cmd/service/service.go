package service

import (
	"errors"
	"fmt"
	"strings"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	serviceenv "github.com/djdv/go-filesystem-utils/cmd/environment/service"
	stopenv "github.com/djdv/go-filesystem-utils/cmd/environment/service/daemon/stop"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/cmd/service/control"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/service/host"
	"github.com/djdv/go-filesystem-utils/cmd/service/status"
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
	Run:      systemServiceRun,
	Options:  parameters.CmdsOptionsFrom((*Settings)(nil)),
	Encoders: formats.CmdsEncoders,
	Type:     daemon.Response{},
	Subcommands: func() map[string]*cmds.Command {
		const staticCommands = 2
		var (
			actions         = service.ControlAction[:]
			controlCommands = control.GenerateCommands(actions...)

			dynamicCommands = len(controlCommands)
			subcommands     = make(map[string]*cmds.Command,
				staticCommands+dynamicCommands)
		)

		// Dynamic Commands:
		for i, action := range actions {
			subcommands[action] = controlCommands[i]
		}

		// Static commands:
		subcommands[status.Name] = status.Command
		subcommands[daemon.Name] = daemon.Command

		return subcommands
	}(),
}

func systemServiceRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	if service.Interactive() {
		return fmt.Errorf(
			"This version of the daemon is intended for use by the host service manager."+
				" Use `%s` for interactive use.",
			strings.Join(append(request.Path, daemon.Name), " "),
		)
	}

	// NOTE: We don't have the system logger yet.
	// These early errors will only show up in tests or a debugger.

	serviceEnv, err := serviceenv.Assert(env)
	if err != nil {
		return err
	}

	ctx := request.Context
	settings, err := parseSettings(ctx, request)
	if err != nil {
		return err
	}
	serviceConfig := host.ServiceConfig(&settings.Host)

	// NOTE: The service API doesn't give us a way to get
	// the system logger without the service interface.
	// So we have to initialize in a cyclical manner.
	// Constructing the interface from the struct,
	// in order to fetch the logger via its method,
	// then assigning the logger to the struct (further below).
	serviceInterface := &daemonCmdWrapper{
		serviceRequest: request,
		emitter:        emitter,
		environment:    serviceEnv,
	}

	serviceController, err := service.New(serviceInterface, serviceConfig)
	if err != nil {
		return err
	}

	sysLog, err := serviceController.SystemLogger(nil)
	if err != nil {
		return err
	}

	// NOTE: Below this line,
	// errors get logged to `sysLog` internally by called functions/methods.
	// FIXME: change this^ syslistenrs got reworked

	_, maddrsProvided := request.Options[fscmds.ServiceMaddrs().CommandLine()]
	serviceListeners, cleanup, err := systemListeners(maddrsProvided, sysLog)
	if err != nil {
		return err
	}

	serviceInterface.sysLog = sysLog
	serviceInterface.sysListeners = serviceListeners

	err = serviceController.Run()

	if cleanupErr := cleanup(); cleanupErr != nil {
		if err == nil {
			err = cleanupErr
		} else {
			err = fmt.Errorf("%w - %s", err, cleanupErr)
		}
	}
	return err
}

type (
	daemonCmdWrapper struct {
		serviceRequest *cmds.Request
		emitter        cmds.ResponseEmitter
		environment    serviceenv.Environment

		sysLog  service.Logger
		runErrs <-chan error
		cleanup func() error

		sysListeners []manet.Listener
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

// TODO: comments may be out of date - we refactored things. Make a pass.
func (svc *daemonCmdWrapper) Start(svcIntf service.Service) error {
	if svc.runErrs != nil {
		return errors.New("service already started")
	}

	var (
		sysLog         = svc.sysLog
		serviceRequest = svc.serviceRequest
		ctx            = serviceRequest.Context
		// NOTE: We use the absolute root of the request
		// rather than the request's relative command.
		// This is because the daemon will serve whatever the request's Root is.
		// And thus impact request paths for other command, such as `service daemon stop`.
		daemonCmdPath = []string{Name, daemon.Name}
	)

	daemonRequest, err := cmds.NewRequest(ctx, daemonCmdPath,
		serviceRequest.Options, nil, nil, serviceRequest.Root)
	if err != nil {
		return logErr(sysLog, err)
	}

	// TODO: HACK
	// This is not a real solution, we need to do arguments properly (later)
	extra := new(cmds.Extra)
	extra.SetValue("magic", svc.sysListeners)
	daemonRequest.Command.Extra = extra
	//

	// The daemon command should block until the daemon stops listening
	// or encounters an error. Emitting responses while it runs.
	daemonEmitter, daemonResponse := cmds.NewChanResponsePair(daemonRequest)
	go serviceRequest.Root.Call(daemonRequest, daemonEmitter, svc.environment)

	// Handle the responses from the daemon in 2 phases.
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

	if err := startup(); err != nil {
		return logErr(sysLog, err)
	}

	// Daemon started successfully.
	// Handle any post-init messages from the daemon in the background.
	runErrs := make(chan error, 1)
	go func() {
		defer close(runErrs)
		if runErr := runtime(); err != nil {
			select {
			case runErrs <- runErr:
			case <-ctx.Done():
				return
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

func (svc *daemonCmdWrapper) Stop(svcIntf service.Service) error {
	var (
		runErrs = svc.runErrs
		sysLog  = svc.sysLog
	)
	if runErrs == nil {
		return logErr(sysLog, errors.New("service wasn't started"))
	}
	svc.runErrs = nil

	var (
		err     error
		stopper = svc.environment.Daemon().Stopper()
	)
	select {
	// If during runtime, env's Stop method was called
	// or an error was encountered this case should not block.
	case runErr, ok := <-runErrs:
		if !ok {
			// Daemon stopped gracefully already.
			// Returning here stops the system service.
			return nil
		}
		// Buffer the first error we got,
		// there may be more coming from the channel.
		err = logErr(sysLog, runErr)

	// Otherwise, we'll call env's Stop method ourself
	// which will indirectly unblock runErr.
	default:
		if stopErr := stopper.Stop(stopenv.Requested); stopErr != nil {
			return logErr(sysLog, stopErr)
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
