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
	cleanupFunc func() error

	runEnvironment struct {
		cancel         context.CancelFunc
		runErrs        <-chan error
		serviceCleanup cleanupFunc
	}

	serviceContextPair struct {
		context.Context
		Cancel context.CancelFunc
	}
)

func (re *runEnvironment) Stop() error {
	re.cancel()

	// TODO: Don't use this prefix if we only get context errors.
	var runtimeErrors error
	for runErr := range re.runErrs {
		if runtimeErrors == nil {
			runtimeErrors = fmt.Errorf("service encountered errors during runtime: %w", runErr)
		} else {
			runtimeErrors = fmt.Errorf("%w - %s", runtimeErrors, runErr)
		}
	}

	// Maybe cleanup any resources allocated in Start.
	var cleanupErrors error
	if cleanup := re.serviceCleanup; cleanup != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			cleanupErrors = fmt.Errorf("service cleanup error: %w", cleanupErr)
		}
	}

	var err error
	for _, e := range []error{runtimeErrors, cleanupErrors} {
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

type serviceDaemon struct {
	context.Context
	*fscmds.Settings
	logger          logger
	cmdsEnvironment cmds.Environment

	// Shared storage for use by `Start` and `Stop`.
	runEnvironment   *runEnvironment
	runEnvironmentMu sync.Mutex

	// For interactive service mode.
	// See: daemon.runContext
	contextChan chan serviceContextPair
}

func newDaemon(ctx context.Context,
	settings *fscmds.Settings, env cmds.Environment) *serviceDaemon {
	return &serviceDaemon{
		Context:         ctx,
		Settings:        settings,
		cmdsEnvironment: env,
		contextChan:     make(chan serviceContextPair, 1),
	}
}

// Start initializes any servers the daemon will use, and launches a run routine around them.
// The run routine serves incoming requests, and exits when either `Stop` is called
// or a service runtime error is encountered.
func (daemon *serviceDaemon) Start(s service.Service) error {
	daemon.runEnvironmentMu.Lock()
	defer daemon.runEnvironmentMu.Unlock()

	if err := daemon.startCheck(); err != nil {
		daemon.logger.Errorf("Start requested but %w", err)
		return err
	}
	if err := daemon.logger.Starting(); err != nil {
		return err
	}

	listeners, serviceCleanup, err := getServiceListeners(daemon.ServiceMaddrs...)
	if err != nil {
		daemon.logger.Errorf("Service initialization error: %w", err)
		return err
	}

	var (
		clientRoot = &cmds.Command{
			Options: fscmds.RootOptions(),
			Helptext: cmds.HelpText{
				Tagline: "File system service client.",
			},
		}
		serviceContext, serviceCancel = context.WithCancel(daemon.Context)

		runContext, runCancel = context.WithCancel(serviceContext)
		runErrs               = make(chan error)
		runWg                 sync.WaitGroup
		runCloser             = func() {
			go func() {
				runWg.Wait()
				close(runErrs)
			}()
		}

		// TODO: extract; these helpers probably don't need to be define right here
		watchContexts = func() {
			runWg.Add(1)
			go func() {
				defer runWg.Done()
				select {
				case <-serviceContext.Done():
					// Service.Stop was called.
					// Don't consider this an error.
				case <-runContext.Done():
					ctxErr := runContext.Err()
					daemon.logger.Warning("Interrupt: ", ctxErr)
					// Start's caller canceled.
					// Return the context error to them.
					runErrs <- ctxErr
				}
			}()
		}

		watchIdle = func() {
			idleErrs, serviceStopInterval := daemon.stopIfNotBusy(runContext)
			if idleErrs == nil {
				return
			}
			daemon.logger.Infof("Requested to stop if not busy every %s",
				serviceStopInterval)

			runWg.Add(1)
			go func() {
				defer runWg.Done()
				for err := range idleErrs {
					daemon.logger.Errorf("Service idle-watcher err: %w", err)
					runCancel()
					runErrs <- err
				}
			}()
		}

		watchServers = func() {
			for _, listener := range listeners {
				serverErrs := acceptCmdsHTTP(serviceContext,
					listener, clientRoot, daemon.cmdsEnvironment)
				runWg.Add(1)
				go func() {
					defer runWg.Done()
					for err := range serverErrs {
						daemon.logger.Errorf("HTTP server error: %w", err)
						runErrs <- err
					}
				}()

				daemon.logger.Listener(listener.Multiaddr())
			}
		}
	)

	defer runCloser()
	watchContexts()
	watchIdle()
	watchServers()

	// For use by the daemon via `Stop`.
	daemon.runEnvironment = &runEnvironment{
		cancel:         serviceCancel,
		runErrs:        runErrs,
		serviceCleanup: serviceCleanup,
	}
	defer func() {
		// For use by callers (optionally) via `waitForRun`.
		select {
		case daemon.contextChan <- serviceContextPair{
			runContext,
			runCancel,
		}:
		case <-daemon.Context.Done():
		default:
		}
	}()
	return daemon.logger.Ready()
}

// TODO: placeholder - move to _platform.go e.g. _systemd.go
// { return systemd.activation.Listeners()}
func maybePlatformListeners() ([]manet.Listener, error) {
	return nil, nil
}

func (daemon *serviceDaemon) startCheck() error {
	if err := daemon.Context.Err(); err != nil {
		return err
	}
	if daemon.runEnvironment != nil {
		return errors.New("service already running")
	}
	return nil
}

func listen(serviceMaddrs ...multiaddr.Multiaddr) ([]manet.Listener, error) {
	serviceListeners := make([]manet.Listener, len(serviceMaddrs))
	for i, maddr := range serviceMaddrs {
		listener, err := manet.Listen(maddr)
		if err != nil {
			err = fmt.Errorf("could not create service listener for %v: %w",
				maddr, err)
			// On failure, close what we opened so far.
			for _, listener := range serviceListeners[:i] {
				if lErr := listener.Close(); lErr != nil {
					err = fmt.Errorf("%w - could not close %s: %s",
						err, listener.Multiaddr(), lErr)
				}
			}
			return nil, err
		}
		serviceListeners[i] = listener
	}
	return serviceListeners, nil
}

func acceptCmdsHTTP(ctx context.Context,
	listener manet.Listener, clientRoot *cmds.Command,
	env cmds.Environment) (serverErrs <-chan error) {
	var (
		httpServer = &http.Server{
			Handler: cmdshttp.NewHandler(env,
				clientRoot, cmdshttp.NewServerConfig()),
		}
		httpServerErrs = make(chan error, 1)
	)
	go func() {
		const stopGrace = 30 * time.Second
		defer close(httpServerErrs)
		serveErr := make(chan error, 1)
		// The actual listen and serve / accept loop.
		go func() { serveErr <- httpServer.Serve(manet.NetListener(listener)) }()
		// Context handling to cancel the server mid `Serve`,
		// and relay errors.

		// FIXME: when the context is done we're not waiting for Serve to return
		select {
		case err := <-serveErr:
			httpServerErrs <- err
		case <-ctx.Done():
			timeout, timeoutCancel := context.WithTimeout(context.Background(),
				stopGrace/2)
			defer timeoutCancel()
			if err := httpServer.Shutdown(timeout); err != nil {
				httpServerErrs <- err
			}
			select {
			case <-timeout.Done():
				httpServerErrs <- fmt.Errorf("could not shutdown server before timeout (%s): %w",
					timeout, timeout.Err())
			case err := <-serveErr:
				if !errors.Is(err, http.ErrServerClosed) {
					httpServerErrs <- err
				}
			}
		}
	}()

	return httpServerErrs
}

// TODO: this should probably return a pkg specific error value. (ErrAutoShutdown)
//
// stopIfNotBusy checks every interval to see if the service is busy.
// If it's not, context.DeadlineExceeded will be sent to the channel.
// Otherwise, the service will be checked again next interval.
// (If a service error is encountered, it will be sent to the channel.)
func (daemon *serviceDaemon) stopIfNotBusy(ctx context.Context) (<-chan error, time.Duration) {
	serviceStopInterval := daemon.Settings.AutoExitInterval
	if serviceStopInterval == 0 {
		return nil, 0
	}

	var (
		stopTicker = time.NewTicker(serviceStopInterval)
		busyErrs   = make(chan error, 1)
		// NOTE [placeholder]: this build is never busy
		checkIfBusy     = func() (bool, error) { return false, nil }
		queryTheService = func() {
			defer stopTicker.Stop()
			defer close(busyErrs)
			for {
				select {
				case <-stopTicker.C:
					busy, err := checkIfBusy()
					if err != nil {
						busyErrs <- err
						return
					}
					if !busy {
						busyErrs <- context.DeadlineExceeded
						return
					}
				case <-ctx.Done():
					return
				}
			}
		}
	)
	go queryTheService()
	return busyErrs, serviceStopInterval
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

	// Retrieve the shared memory set in Start.
	runEnv := daemon.runEnvironment
	if runEnv == nil {
		err := errors.New("service is not running")
		daemon.logger.Errorf("Stop requested but %w", err)
		return err
	}
	defer func() { daemon.runEnvironment = nil }()

	daemon.logger.Info("Stopping...")
	defer daemon.logger.Info("Stopped")

	select {
	case <-daemon.contextChan:
		// The context pair sent during `Start`,
		// was never received. Clear the channel.
		// (This is expected to happen in service mode.)
	default:
		// Someone received the context pair.
		// (This is expected to happen in interactive mode.)
	}

	if err := runEnv.Stop(); err != nil {
		if !errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) {
			daemon.logger.Errorf("Encountered error while stopping: %w", err)
		}
		return err
	}

	return nil
}
