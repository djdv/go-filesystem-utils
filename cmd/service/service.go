package service

import (
	"errors"
	"fmt"

	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	cmdsenv "github.com/djdv/go-filesystem-utils/internal/cmds/environment"
	"github.com/djdv/go-filesystem-utils/internal/cmds/environment/stop"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
	manet "github.com/multiformats/go-multiaddr/net"
)

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

	stopper := svc.environment.Stopper()
	select {
	case daemonErr, running := <-daemonErrs:
		if !running {
			return nil
		}
		err = daemonErr
	default:
		if stopErr := stopper.Stop(stop.Requested); stopErr != nil {
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
