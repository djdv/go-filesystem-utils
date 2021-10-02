package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment"
	daemonenv "github.com/djdv/go-filesystem-utils/cmd/ipc/environment/daemon"
	serviceenv "github.com/djdv/go-filesystem-utils/cmd/ipc/environment/service"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/cmd/service/control"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/service/status"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
	manet "github.com/multiformats/go-multiaddr/net"
)

const Name = "service"

var Command = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: serviceenv.ServiceDescription,
	},
	NoRemote: true,
	Run:      systemServiceRun,
	Options:  parameters.CmdsOptionsFrom((*Settings)(nil)),
	Encoders: formats.CmdsEncoders,
	Type:     ipc.ServiceResponse{},
	Subcommands: func() map[string]*cmds.Command {
		var (
			actions     = service.ControlAction[:]
			controls    = control.GenerateCommands(actions...)
			subcommands = make(map[string]*cmds.Command, len(controls)+1)
		)
		subcommands[status.Name] = status.Command
		subcommands[daemon.Name] = daemon.Command
		for i, action := range actions {
			subcommands[action] = controls[i]
		}
		return subcommands
	}(),
}

func systemServiceRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	if service.Interactive() {
		return fmt.Errorf("This version of the daemon is intended for use by the host service manager"+
			"use `%s` for interactive use", strings.Join(append(request.Path, daemon.Name), " "),
		)
	}

	// NOTE: We don't have the system logger yet.
	// These early errors will only show up in tests or a debugger.
	fsEnv, err := environment.CastEnvironment(env)
	if err != nil {
		return err
	}
	serviceConfig, err := fsEnv.Service().Config(request)
	if err != nil {
		return err
	}

	// NOTE: The service API doesn't give us a way to get
	// the system logger without the service interface.
	// So we have to initialize in a cyclical manner.
	// Constructing the interface from the struct,
	// in order to fetch the logger via its method,
	// then assigning the logger to the struct (further below).
	serviceInterface := &daemonCmdWrapper{
		serviceRequest: request,
		emitter:        emitter,
		environment:    fsEnv,
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
		environment    environment.Environment
		sysListeners   []manet.Listener

		sysLog  service.Logger
		runErrs <-chan error
		cleanup func() error
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
		sysLog        = svc.sysLog
		ctx           = svc.serviceRequest.Context
		daemonCmdPath = append(svc.serviceRequest.Path, daemon.Name)
	)

	daemonRequest, err := cmds.NewRequest(ctx, daemonCmdPath,
		svc.serviceRequest.Options, nil, nil, svc.serviceRequest.Root)
	if err != nil {
		return logErr(sysLog, err)
	}

	// TODO: HACK
	// This is not a real solution, we need to do arguments properly (later)
	extra := new(cmds.Extra)
	extra.SetValue("magic", svc.sysListeners)
	daemonRequest.Command.Extra = extra
	//

	// TODO: comments may be out of date - we refactored things. Make a pass.

	// The daemon command should block until the daemon stops listening
	// or encounters an error. Emitting responses while it runs.
	daemonEmitter, daemonResponse := cmds.NewChanResponsePair(daemonRequest)
	go svc.serviceRequest.Root.Call(daemonRequest, daemonEmitter, svc.environment)

	// Setup a response channel for the daemon to send responses to.
	// TODO: figure out what context to use for this. It can't be the request context.
	// When that's canceled, next will return its canceled error value, which we want on Errs.
	daemonResponses, daemonErrs := daemonenv.ParseResponse(context.TODO(), daemonResponse)

	if err := handleDaemonInit(sysLog, daemonResponses, daemonErrs); err != nil {
		return err
	}

	// Daemon started successfully.
	// Handle any post-init messages from the daemon in the background.
	// Store the error channel so that `Stop`
	// may return any errors encountered during runtime.
	svc.runErrs = handleDaemonRun(sysLog, svcIntf, daemonResponses, daemonErrs)

	return nil
}

// handleDaemonInit logs daemon responses to the system logger
// and waits for the daemon to report that it's ready.
func handleDaemonInit(sysLog service.Logger,
	daemonResponses <-chan daemonenv.Response, daemonErrs <-chan error) error {
	for {
		select {
		case response := <-daemonResponses:
			if err := sysLog.Info(response.String()); err != nil {
				return logErr(sysLog, err)
			}
			if response.Status == daemonenv.Ready {
				return nil
			}
		case err := <-daemonErrs:
			return logErr(sysLog, err)
		}
	}
}

func handleDaemonRun(sysLog service.Logger, svcIntf service.Service,
	daemonResponses <-chan daemonenv.Response, daemonErrs <-chan error) <-chan error {
	runErrs := make(chan error, 1)
	go func() {
		defer close(runErrs)
		var sawStopResponse bool
	out:
		for daemonResponses != nil ||
			daemonErrs != nil {
			select {
			case response, ok := <-daemonResponses:
				if !ok {
					daemonResponses = nil
					continue
				}
				if err := sysLog.Info(response.String()); err != nil {
					runErrs <- err
					break out
				}
				if response.Status == daemonenv.Stopping {
					sawStopResponse = true
				}
			case err, ok := <-daemonErrs:
				if !ok {
					daemonErrs = nil
					continue
				}
				runErrs <- fmt.Errorf("daemon response error: %w", err)
				break out
			}
		}
		if !sawStopResponse {
			// Call `service.Service.Stop` (<- service controller's request method)
			// This will unblock `service.Service.Run`
			// which will then call `service.Interface.Stop` (<- our implementation)
			// allowing the service process to drain `runErrs` and exit.
			//
			// NOTE: If the system refuses our request to Stop
			// there's nothing we can do about it other than try to log it.
			// (This is unlikely to happen, but technically possible)
			// The system operator will have to stop us manually via the host itself.
			// (`Stop-Service`, `service stop`, `svcadm disable`, `launchctl unload`, etc.)
			if svcErr := svcIntf.Stop(); svcErr != nil {
				sysLog.Error(svcErr)
			}
		}
	}()
	return runErrs
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

	select {
	case err := <-runErrs:
		// If runtime errors were encountered, the daemon is already stopping/stopped.
		return err
	default:
		// Otherwise, stop it gracefully.
		if stopErr := svc.environment.Daemon().Stop(daemonenv.StopRequested); stopErr != nil {
			return stopErr
		}
		// Wait for daemon.Run to return.
		return <-runErrs
	}
}
