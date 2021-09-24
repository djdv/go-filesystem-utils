package service

import (
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

	// NOTE: We don't have the system logger right away.
	// Early errors will only show up in tests or a debugger.
	fsEnv, err := environment.CastEnvironment(env)
	if err != nil {
		return err
	}
	serviceConfig, err := fsEnv.Service().Config(request)
	if err != nil {
		return err
	}

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

	_, maddrsProvided := request.Options[fscmds.ServiceMaddrs().CommandLine()]
	serviceListeners, cleanup, err := systemListeners(maddrsProvided)
	if err != nil {
		return logErr(sysLog, err)
	}

	serviceInterface.sysLog = sysLog
	serviceInterface.sysListeners = serviceListeners

	// NOTE: Run logs errors itself internally
	err = serviceController.Run()

	if cleanupErr := cleanup(); cleanupErr != nil {
		cleanupErr = logErr(sysLog, cleanupErr)
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

	// The daemon command should block until the daemon stops listening
	// or encounters an error. Emitting responses while it runs.
	daemonEmitter, daemonResponse := cmds.NewChanResponsePair(daemonRequest)
	go svc.serviceRequest.Root.Call(daemonRequest, daemonEmitter, svc.environment)

	// Parse the initialization responses to assure things are okay server-side.
	responseHandler := daemonResponseHandler(sysLog)
	if initErr := daemonenv.HandleInitSequence(daemonResponse, responseHandler); initErr != nil {
		return logErr(sysLog, initErr)
	}

	// Handle any post-init messages from the daemon in the background.
	runErrs := make(chan error, 1)
	go func() {
		defer close(runErrs)
		if err := daemonenv.HandleRunningSequence(daemonResponse, responseHandler); err != nil {
			// Daemon encountered an error
			// buffer it for the `service.Interface.Stop` method.
			runErrs <- fmt.Errorf("daemon response error: %w", err)
			// Call `service.Service.Stop` (<- host interface method)
			// This will unblock `service.Service.Run`
			// which will then call `service.Interface.Stop` (<- our method)
			// allowing the service process to exit.
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

	// We've started successfully.
	// Store the error channel so that `Stop`
	// may return any errors encountered during runtime.
	svc.runErrs = runErrs
	return nil
}

func daemonResponseHandler(sysLog service.Logger) daemonenv.ResponseHandlerFunc {
	return func(response *daemonenv.Response) (err error) {
		switch response.Status {
		case daemonenv.Starting:
			err = sysLog.Info(ipc.StdoutHeader)
		case daemonenv.Ready:
			if encodedMaddr := response.ListenerMaddr; encodedMaddr != nil {
				err = sysLog.Infof("%s %s", ipc.StdoutListenerPrefix, encodedMaddr.Interface)
			} else {
				err = sysLog.Info(ipc.StdServerReady)
			}
		case daemonenv.Stopping:
			err = sysLog.Infof("Stopping: %s", response.StopReason.String())
		case daemonenv.Error:
			if errMsg := response.Info; errMsg == "" {
				err = errors.New("service responded with an error status, but no message")
			} else {
				err = errors.New(errMsg)
			}
			err = logErr(sysLog, err)
		default:
			if response.Info != "" {
				err = sysLog.Info(response.Info)
			} else {
				err = logErr(sysLog,
					fmt.Errorf("service responded with an unexpected response: %#v",
						response,
					),
				)
			}
		}
		return
	}
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
