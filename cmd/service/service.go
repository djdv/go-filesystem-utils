package service

import (
	"errors"
	"fmt"

	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment"
	daemonenv "github.com/djdv/go-filesystem-utils/cmd/ipc/environment/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/executor"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/cmd/service/control"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/service/status"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
	manet "github.com/multiformats/go-multiaddr/net"
)

const Name = ipc.ServiceCommandName

var Command = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: ipc.ServiceDescription,
	},
	NoRemote: true,
	Run:      systemServiceRun,
	PostRun: cmds.PostRunMap{
		cmds.CLI: formatSystemService,
	},
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
		return errors.New("This version of the daemon is intended for use by the host service manager" +
			"use `service daemon` for interactive use",
		) // TODO: we need to get the dynamic cmdsPath from a function
	}

	// NOTE: We can't get the systemlogger yet
	// So these errors aren't going to show up anywhere besides a debugger.

	fsEnv, err := environment.CastEnvironment(env)
	if err != nil {
		return err
	}
	serviceConfig, err := fsEnv.Service().Config(request)
	if err != nil {
		return err
	}

	serviceInterface := &cmdsServiceWrapper{
		request:     request,
		emitter:     emitter,
		environment: env,
	}

	serviceController, err := service.New(serviceInterface, serviceConfig)
	if err != nil {
		return err
	}

	sysLog, err := serviceController.SystemLogger(nil)
	if err != nil {
		return err
	}

	serviceListeners, cleanup, err := systemListeners()
	if err != nil {
		return logErr(sysLog, err)
	}

	serviceInterface.sysLog = sysLog
	serviceInterface.sysListeners = serviceListeners

	var (
		runErr     = serviceController.Run()
		cleanupErr = cleanup()
		loggerErr  error
	)
	if cleanupErr != nil {
		loggerErr = logErr(sysLog, cleanupErr)
	}

	{
		var err error
		for _, e := range []error{runErr, cleanupErr, loggerErr} {
			switch {
			case e == nil:
				continue
			case err == nil:
				err = e
			case err != nil:
				err = fmt.Errorf("%w\n%s", err, e)
			}
		}
		return err
	}
}

type (
	cmdsServiceWrapper struct {
		request      *cmds.Request
		emitter      cmds.ResponseEmitter
		environment  cmds.Environment
		sysListeners []manet.Listener

		sysLog  service.Logger
		runErrs <-chan error
		cleanup func() error
	}
)

func logErr(logger service.Logger, err error) error {
	if logErr := logger.Error(err); logErr != nil {
		return fmt.Errorf("%w - %s", err, logErr)
	}
	return err
}

func (svc *cmdsServiceWrapper) Start(svcIntf service.Service) error {
	if svc.runErrs != nil {
		return errors.New("already started") // TODO: err msg
	}

	var (
		sysLog = svc.sysLog
		ctx    = svc.request.Context
	)

	// TODO: use the function to get the cmd path
	daemonRequest, err := cmds.NewRequest(ctx, []string{"service", "daemon"},
		svc.request.Options, nil, nil, svc.request.Root)
	if err != nil {
		return logErr(sysLog, err)
	}
	// HACK: This is not a real solution, we need to do arguments properly (later)
	extra := new(cmds.Extra)
	extra.SetValue("magic", svc.sysListeners)
	//daemonRequest.Command.Extra = extra

	inprocExecutor, err := executor.MakeExecutor(daemonRequest, svc.environment)
	if err != nil {
		return logErr(sysLog, err)
	}

	var (
		daemonEmitter, daemonResponse = cmds.NewChanResponsePair(daemonRequest)
		execChan                      = make(chan error)

		initChan        = make(chan error)
		responseHandler = func(response *daemonenv.Response) (err error) {
			switch response.Status {
			case daemonenv.Starting:
				err = sysLog.Info(ipc.StdHeader)
			case daemonenv.Ready:
				if encodedMaddr := response.ListenerMaddr; encodedMaddr != nil {
					err = sysLog.Infof("%s %s", ipc.StdGoodStatus, encodedMaddr.Interface)
				} else {
					err = sysLog.Info(ipc.StdReady)
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
			return err
		}
	)

	// We're going to `Execute` the daemon command,
	// which should block until the daemon stops listening.
	// The daemon will emit responses while it's running, before `Execute` returns.
	go func() {
		defer close(execChan)
		execChan <- inprocExecutor.Execute(daemonRequest, daemonEmitter, svc.environment)
	}()
	go func() {
		defer close(initChan)
		initChan <- daemonenv.HandleInitSequence(daemonResponse, responseHandler)
	}()

	// We check to make sure exec doesn't return early with an error,
	// as well as checking the daemon init sequence.
	var initErr error
	select {
	case initErr = <-execChan: // Failed to execute.
	case initErr = <-initChan: // Daemon initialization error.
	}
	if initErr != nil {
		return logErr(sysLog, initErr)
	}

	// Handle any post-init messages from the daemon.
	daemonErrs := make(chan error)
	go func() {
		defer close(daemonErrs)
		if err := daemonenv.HandleRunningSequence(daemonResponse, responseHandler); err != nil {
			daemonErrs <- err
		}
	}()

	// Splice errors from the exec call with the daemon responder.
	runErrs := make(chan error)
	go func() {
		defer close(runErrs)
		for daemonErrs != nil &&
			execChan != nil {
			var (
				err error
				ok  bool
			)
			select {
			case err, ok = <-daemonErrs:
				if !ok {
					daemonErrs = nil
					continue
				}
				err = fmt.Errorf("daemon response error: %w", err)
			case err, ok = <-execChan:
				if !ok {
					execChan = nil
					continue
				}
				err = fmt.Errorf("execute error: %w", err)
			}
			runErrs <- err
		}
	}()

	// We've started successfully.
	// Store the error channel so that `Stop`
	// may return any errors encountered during runtime.
	svc.runErrs = runErrs
	return nil
}

func (svc *cmdsServiceWrapper) Stop(svcIntf service.Service) error {
	var (
		runErrs = svc.runErrs
		sysLog  = svc.sysLog
		ctx     = svc.request.Context
	)
	if runErrs == nil {
		return logErr(sysLog, errors.New("service wasn't started")) // TODO: err msg
	}
	svc.runErrs = nil

	// If runtime errors were encountered, the daemon is already stopping/stopped.
	select {
	case err := <-runErrs:
		for e := range runErrs {
			err = fmt.Errorf("%w - %s", err, e)
		}
		return err
	default:
	}

	// TODO: command path needs to be from a function
	// Ask the daemon to stop gracefully.
	stopRequest, err := cmds.NewRequest(ctx, []string{"service", "daemon", "stop"},
		svc.request.Options, nil, nil, svc.request.Root)
	if err != nil {
		return logErr(sysLog, err)
	}

	// TODO: none of this
	// we can just retain the env, and call stop directly ourselves
	//
	// TODO: we need to execute stop in a goroutine
	// if the daemon.Run already returned (due to an error) this will block forever
	// we need to select on it here
	// select stopErr | runErrs; if runErrs, return bad
	// otherwise, drain runErrs

	var (
		executor = cmds.NewExecutor(svc.request.Root)
		stopErrs = make(chan error)
	)
	go func() {
		defer close(stopErrs)
		if err := executor.Execute(stopRequest, svc.emitter, svc.environment); err != nil {
			stopErrs <- err
		}
	}()

	select {
	case err := <-runErrs:
		return logErr(sysLog, err)
	case err := <-stopErrs:
		if err != nil {
			return logErr(sysLog, err)
		}
	}

	{ // TODO: Gross. Nil+close check should be sender side.
		var err error
		for e := range runErrs {
			switch {
			case e == nil:
				continue
			case err == nil:
				err = e
			case err != nil:
				err = fmt.Errorf("%w\n%s", err, e)
			}
		}
		if err != nil {
			return logErr(sysLog, err)
		}
	}

	return nil
}
