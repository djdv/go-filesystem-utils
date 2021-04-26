package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	cmds "github.com/ipfs/go-ipfs-cmds"
	cmdshttp "github.com/ipfs/go-ipfs-cmds/http"
	"github.com/kardianos/service"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	serviceDaemon struct {
		settings daemonSettings

		cmdsRequest     *cmds.Request
		cmdsEnvironment cmds.Environment

		// Shared storage for use by `Start` and `Stop`.
		runEnvironment   *runEnvironment
		runEnvironmentMu sync.Mutex

		// For interactive service mode.
		// See: daemon.runContext
		contextChan chan serviceContextPair
	}

	cleanupFunc    func() error
	runEnvironment struct {
		serviceCleanup cleanupFunc

		cancel context.CancelFunc
		errs   <-chan error

		server     *http.Server
		serverErrs <-chan error
	}

	serviceContextPair struct {
		context.Context
		Cancel context.CancelFunc
	}
)

func newDaemon(request *cmds.Request, env cmds.Environment,
	options ...daemonOption) (*serviceDaemon, error) {
	return &serviceDaemon{
		settings:        parseDaemonOptions(options...),
		cmdsRequest:     request,
		cmdsEnvironment: env,
		contextChan:     make(chan serviceContextPair, 1),
	}, nil
}

type (
	daemonOption interface{ apply(*daemonSettings) }

	daemonSettings struct {
		stopInterval time.Duration
	}

	stopIntervalOpt time.Duration
)

func parseDaemonOptions(options ...daemonOption) daemonSettings {
	settings := new(daemonSettings)
	for _, opt := range options {
		opt.apply(settings)
	}
	return *settings
}

func getDaemonArguments(request *cmds.Request) ([]daemonOption, error) {
	stopInterval, provided, err := fscmds.GetDurationArgument(request, fscmds.AutoExitParameter)
	if err != nil {
		return nil, err
	}

	var daemonOptions []daemonOption
	if provided {
		daemonOptions = append(daemonOptions, withStopInterval(stopInterval))
	}
	return daemonOptions, nil
}

// withStopInterval will cause the daemon to check if it's busy every duration.
// If the daemon is not busy at that time, it will stop running.
func withStopInterval(d time.Duration) daemonOption      { return stopIntervalOpt(d) }
func (d stopIntervalOpt) apply(settings *daemonSettings) { settings.stopInterval = time.Duration(d) }

// Start initializes any servers the daemon will use, and launches a run routine around them.
// The run routine serves incoming requests, and exits when either `Stop` is called
// or a service runtime error is encountered.
func (daemon *serviceDaemon) Start(s service.Service) error {
	daemon.runEnvironmentMu.Lock()
	defer daemon.runEnvironmentMu.Unlock()

	logger, err := s.Logger(nil)
	if err != nil {
		return err
	}

	if err := daemon.startCheck(); err != nil {
		logger.Errorf("Start requested but %s", err)
		return err
	}
	logger.Info(stdHeader)

	serviceListener, serviceCleanup, err := daemon.serverInit()
	if err != nil {
		logger.Errorf("Service initialization error: %s", err)
		return err
	}
	logger.Info(stdGoodStatus, serviceListener.Multiaddr())

	stopInterval := daemon.settings.stopInterval
	daemon.serviceLaunch(serviceListener, serviceCleanup, stopInterval)
	if stopInterval != 0 {
		logger.Infof("Requested to stop if not busy every %s", stopInterval)
	}
	if service.Interactive() {
		logger.Info("Send interrupt to stop")
	}

	return logger.Info(stdReady)
}

func (daemon *serviceDaemon) startCheck() error {
	if err := daemon.cmdsRequest.Context.Err(); err != nil {
		return err
	}
	if daemon.runEnvironment != nil {
		return errors.New("service already running")
	}
	return nil
}

func (daemon *serviceDaemon) serverInit() (serviceListener manet.Listener,
	serviceCleanup cleanupFunc, err error) {
	var (
		serviceMaddr multiaddr.Multiaddr
		provided     bool
	)
	serviceMaddr, provided, err = fscmds.GetMultiaddrArgument(daemon.cmdsRequest,
		fscmds.ServiceMaddrParameter)
	if !provided {
		serviceMaddr, serviceCleanup, err = localServiceMaddr()
	}
	if err != nil {
		return
	}

	serviceListener, err = manet.Listen(serviceMaddr)
	if err != nil {
		err = fmt.Errorf("could not create service listener: %w", err)
	}

	return
}

func (daemon *serviceDaemon) serviceLaunch(serviceListener manet.Listener, serviceCleanup cleanupFunc, stopInterval time.Duration) {
	var (
		clientsRoot = &cmds.Command{
			Options: fscmds.RootOptions(),
			Helptext: cmds.HelpText{
				Tagline: "File system service client.",
			},
		}
		httpServer, serverErrors = serveCmdHTTP(serviceListener,
			clientsRoot, daemon.cmdsEnvironment)

		// serviceCancel available to `Stop`
		serviceContext, serviceCancel = context.WithCancel(context.Background())
		// runCancel indirectly available to the caller via `daemon.waitForRun`
		runContext, runCancel, runErrs = serviceRun(serviceContext, stopInterval, serverErrors)

		// Shared memory for `serviceShutdown`.
		// Gives cancel access of `serviceRun`,
		// and all error channels.
		runEnv = &runEnvironment{
			serviceCleanup: serviceCleanup,

			server:     httpServer,
			serverErrs: serverErrors,

			cancel: serviceCancel,
			errs:   runErrs,
		}

		// Shared memory for interactive use.
		// Restricted set of above.
		runtimeCancelContext = serviceContextPair{
			runContext,
			runCancel,
		}
	)

	daemon.runEnvironment = runEnv // For use by the daemon via `Stop`.
	select {                       // For use by callers (optionally) via `waitForRun`.
	case daemon.contextChan <- runtimeCancelContext:
	case <-daemon.cmdsRequest.Context.Done():
	default:
	}
}

func serviceRun(ctx context.Context, stopInterval time.Duration,
	serverErrors <-chan error) (context.Context, context.CancelFunc, <-chan error) {
	var (
		stopErrors        = stopIfNotBusy(ctx, stopInterval)
		runErrs           = make(chan error, 1)
		runCtx, runCancel = context.WithCancel(ctx)
	)
	go func() {
		defer func() {
			close(runErrs)
			runCancel()
		}()
		select {
		case serverErr := <-serverErrors:
			// Server encountered error while running.
			runErrs <- serverErr
		case stopErr := <-stopErrors:
			// Service requested to shutdown when idle.
			runErrs <- stopErr
		case <-ctx.Done():
			// Caller was canceled.
		case <-runCtx.Done():
			// Caller explicitly canceled us.
			runErrs <- runCtx.Err()
		}
	}()
	return runCtx, runCancel, runErrs
}

func serveCmdHTTP(listener manet.Listener, clientRoot *cmds.Command,
	env cmds.Environment) (*http.Server, <-chan error) {
	var (
		httpServeError = make(chan error, 1)
		httpServer     = &http.Server{
			Handler: cmdshttp.NewHandler(env,
				clientRoot, cmdshttp.NewServerConfig()),
		}
	)
	go func() {
		defer close(httpServeError)
		httpServeError <- httpServer.Serve(manet.NetListener(listener))
	}()
	return httpServer, httpServeError
}

// TODO: this should probably return a pkg specific error value. (ErrAutoShutdown)
//
// stopIfNotBusy checks every interval to see if the service is busy.
// If it's not, context.DeadlineExceeded will be sent to the channel.
// Otherwise, the service will be checked again next interval.
// (If a service error is encountered, it will be sent to the channel.)
func stopIfNotBusy(ctx context.Context, checkInterval time.Duration) <-chan error {
	if checkInterval == 0 {
		return nil
	}
	var (
		stopTicker = time.NewTicker(checkInterval)
		busyErrs   = make(chan error, 1)
		// NOTE [placeholder]: this build is never busy
		checkIfBusy     = func() (bool, error) { return false, nil }
		queryTheService = func() {
			defer stopTicker.Stop()
			for {
				select {
				case <-stopTicker.C:
					busy, err := checkIfBusy()
					if err != nil {
						busyErrs <- err
						close(busyErrs)
						return
					}
					if !busy {
						busyErrs <- context.DeadlineExceeded
						close(busyErrs)
						return
					}

				case <-ctx.Done():
					// NOTE:
					// busyErrs now blocks forever
					return
				}
			}
		}
	)
	go queryTheService()
	return busyErrs
}

// waitForRun should be called immediately after a successful call to `Start`,
// when managing the service in interactive mode.
//
// Blocking until the passed in context is canceled;
// in which case run's CancelFunc is also called.
// Or until the service encounters a runtime error.
//
// In either case, `Stop` must be called after `waitForRun` returns,
// before another call to `Start` will succeed.
func (daemon *serviceDaemon) waitForRun(ctx context.Context) context.Context {
	var serviceRun serviceContextPair
	// wait for Start's routine to relay its context pair
	select {
	case serviceRun = <-daemon.contextChan:
	case <-ctx.Done():
		return ctx
	}

	// Start's routine is now running.
	// If the parent context cancels, we will cancel the service.
	// If the service cancels before then, we will just unblock.
	runCtx := serviceRun.Context
	select {
	case <-ctx.Done():
		serviceRun.Cancel()
	case <-runCtx.Done():
	}
	return runCtx
}

// Stop halts the routine launched in `Start`.
// Shutting down any servers it may have launched.
// And returning any errors it may have encountered during runtime or shutdown.
func (daemon *serviceDaemon) Stop(s service.Service) error {
	daemon.runEnvironmentMu.Lock()
	defer daemon.runEnvironmentMu.Unlock()

	logger, err := s.Logger(nil)
	if err != nil {
		return err
	}

	// Retrieve the shared memory set in Start.
	runEnv := daemon.runEnvironment
	if runEnv == nil {
		err := errors.New("service is not running")
		logger.Errorf("Stop requested but %s", err)
		return err
	}
	defer func() { daemon.runEnvironment = nil }()

	logger.Info("Stopping...")
	defer logger.Info("Stopped")

	select {
	case <-daemon.contextChan:
		// The context pair sent during `Start`,
		// was never received. Clear the channel.
		// (This is expected to happen in service mode.)
	default:
		// Someone received the context pair.
		// (This is expected to happen in interactive mode.)
	}

	// Stop the routine started in Start.
	runEnv.cancel()
	var (
		runErr   = <-runEnv.errs
		fatalErr = runErr != nil &&
			!(errors.Is(runErr, context.Canceled) ||
				errors.Is(runErr, context.DeadlineExceeded))
	)
	if fatalErr {
		logger.Errorf("Service encountered runtime error: %s", runErr)
		return fmt.Errorf("service error: %w", runErr)
	}

	// Shut down the server started in Start.
	if serverErr := daemon.serviceShutdown(runEnv.server,
		runEnv.serverErrs); serverErr != nil {
		logger.Errorf("Service shutdown error: %s", serverErr)
		return fmt.Errorf("service shutdown error: %w", serverErr)
	}

	// Maybe cleanup any resources allocated in Start.
	if cleanup := runEnv.serviceCleanup; cleanup != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			logger.Errorf("Cleanup error: %s", cleanupErr)
			return fmt.Errorf("service cleanup error: %w", cleanupErr)
		}
	}

	return runErr
}

func (daemon *serviceDaemon) serviceShutdown(server *http.Server, serverErrs <-chan error) error {
	timeout, timeoutCancel := context.WithTimeout(context.Background(),
		stopGrace/2)
	defer timeoutCancel()

	// NOTE [async]:
	// It's possible for `Shutdown` to be called before `Serve`.
	// Make sure to wait for `Serve` to return,
	// and send its value to `serverErrs`.
	// Before returning ourselves.
	// (Otherwise the listeners from init may remain open)
	var (
		shutdownErr = server.Shutdown(timeout)
		serverErr   = <-serverErrs
	)
	if shutdownErr != nil {
		return fmt.Errorf("http shutdown error: %w", shutdownErr)
	}
	if serverErr != nil &&
		!errors.Is(serverErr, http.ErrServerClosed) {
		return fmt.Errorf("http server error: %w", serverErr)
	}
	return nil
}
