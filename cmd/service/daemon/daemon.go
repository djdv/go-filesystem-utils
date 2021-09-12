package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	cmds "github.com/ipfs/go-ipfs-cmds"
	cmdshttp "github.com/ipfs/go-ipfs-cmds/http"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// TODO: use a value from ipc pkg
//const Name = ipc.ServiceCommandName
const Name = "daemon"

var Command = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: ipc.ServiceDescription,
	},
	NoRemote: true,
	Run:      daemonRun,
	PostRun: cmds.PostRunMap{
		cmds.CLI: formatDaemon,
	},
	Options:  parameters.CmdsOptionsFrom((*Settings)(nil)),
	Encoders: formats.CmdsEncoders,
	Type:     daemon.Response{},
	Subcommands: map[string]*cmds.Command{
		stop.Name: stop.Command,
	},
}

type cleanupFunc func() error

var ErrIdle = errors.New("service exited because it was idle")

func clientRoot() *cmds.Command {
	return &cmds.Command{
		Options: fscmds.RootOptions(),
		Helptext: cmds.HelpText{
			Tagline: "File system service client.",
		},
		Subcommands: map[string]*cmds.Command{
			"service": {
				// TODO: add stub info
				// this command only exists for its path to `stop`
				Subcommands: map[string]*cmds.Command{
					Name: {
						Subcommands: map[string]*cmds.Command{
							stop.Name: stop.Command,
						},
					},
				},
			},
		},
	}
}

func daemonRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	var (
		ctx             = request.Context
		settings        = new(Settings)
		unsetArgs, errs = parameters.ParseSettings(ctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
	)
	if _, err := parameters.AccumulateArgs(ctx, unsetArgs, errs); err != nil {
		return err
	}
	ipcEnv, err := environment.CastEnvironment(env)
	if err != nil {
		return err
	}

	if err := emitter.Emit(&daemon.Response{Status: daemon.Starting}); err != nil {
		return err
	}

	// TODO: emitter loop goes here?
	// some kind of channel returned from runService?
	// if emit errors, stop the service?
	// ^ yeah, let's

	stopWatcher, err := ipcEnv.Daemon().Initialize()
	if err != nil {
		return err
	}

	listeners, cleanup, err := getListeners(request.Command.Extra, settings.ServiceMaddrs...)
	if err != nil {
		return err
	}

	var (
		daemonCtx, daemonCancel  = signal.NotifyContext(ctx, os.Interrupt)
		serverCtx, serverCancel  = context.WithCancel(daemonCtx)
		clientRoot               = clientRoot()
		serverMaddrs, serverErrs = serveCmdsHTTP(serverCtx, clientRoot, env, listeners...)

		wrapAndCleanup = func(err error) error {
			if cleanup != nil {
				if cErr := cleanup(); cErr != nil {
					return fmt.Errorf("%w - could not cleanup: %s", err, cErr)
				}
			}
			return err
		}
		serverWrapAndCleanup = func(err error) error {
			// Cancel the server(s) and wait for listener(s) to close
			// (by blocking on their error channel).
			serverCancel()
			for sErr := range serverErrs {
				err = fmt.Errorf("%w - %s", err, sErr)
			}
			return wrapAndCleanup(err)
		}
	)
	defer daemonCancel()
	defer serverCancel()

	for listenerMaddr := range serverMaddrs {
		if err := emitter.Emit(&daemon.Response{
			Status:        daemon.Ready,
			ListenerMaddr: &formats.Multiaddr{Interface: listenerMaddr},
		}); err != nil {
			serverWrapAndCleanup(err)
		}
	}

	if err := emitter.Emit(&daemon.Response{
		Status: daemon.Ready,
	}); err != nil {
		return serverWrapAndCleanup(err)
	}

	var (
		busyCheckInterval           = settings.AutoExitInterval
		idleSignal, idleWatcherErrs = signalIfNotBusy(daemonCtx, busyCheckInterval, ipcEnv)
	)
	if busyCheckInterval != 0 {
		if err := emitter.Emit(&daemon.Response{
			Info: fmt.Sprintf("Requested to stop if not busy every %s",
				busyCheckInterval),
		}); err != nil {
			return serverWrapAndCleanup(err)
		}
	}

	select {
	case <-ctx.Done():
		err = ctx.Err()
	case reason := <-stopWatcher:
		err = emitter.Emit(&daemon.Response{
			Status: daemon.Stopping,
			Reason: reason,
		})
	case <-idleSignal:
		err = emitter.Emit(&daemon.Response{
			Status: daemon.Stopping,
			Reason: daemon.Idle,
		})
	case err = <-idleWatcherErrs:
	case err = <-serverErrs:
	}

	return serverWrapAndCleanup(err)
}

// TODO: We need a way to pass listeners from `service` to `daemon`.
// Easiest way would be to add a parameter for it of type `[]net.Listener`.
// This would need to be of typed as `[]string` in the cmds.Option,
// which could be in a specific format.
// Anything usable with `os.NewFile`->`net.FileListener`.
// E.g.
// CSV of `file-descriptor $groupseperator  file-name`
// As `fs.exe service daemon --existing-sockets="3:systemdSock1,4:systemdSock2"`
//
// For now, we can sidestep this at the API level and just use command.Extra
// (from within `service`, after copying `daemon` add the listeners, then call execute on that)
// But this is not a proper solution, it's only temporary to not break the existing feature
// while separating the commands implementations.
func getListeners(cmdsExtra *cmds.Extra,
	maddrs ...multiaddr.Multiaddr) ([]manet.Listener, cleanupFunc, error) {
	var listeners []manet.Listener
	cmdsListeners, listenersProvided := cmdsExtra.GetValue("magic")
	if listenersProvided {
		netListeners, ok := cmdsListeners.([]net.Listener)
		if !ok {
			err := fmt.Errorf("Command.Extra value has wrong type"+
				"expected %T"+
				"got: %T",
				netListeners,
				cmdsListeners,
			)
			return nil, nil, err
		}
		listeners = make([]manet.Listener, len(netListeners))
		for i, listener := range netListeners {
			manListener, err := manet.WrapNetListener(listener)
			if err != nil {
				return nil, nil, err
			}
			listeners[i] = manListener
		}
	}

	argListeners, err := listen(maddrs...)
	if err != nil {
		return nil, nil, err
	}
	listeners = append(listeners, argListeners...)

	var cleanup cleanupFunc
	if len(listeners) == 0 {
		listeners, cleanup, err = defaultListeners()
	}

	return listeners, cleanup, err
}

func defaultListeners() ([]manet.Listener, cleanupFunc, error) {
	localDefaults, err := fscmds.UserServiceMaddrs()
	if err != nil {
		return nil, nil, err
	}

	type attempted struct {
		multiaddr.Multiaddr
		error
	}

	tried := make([]attempted, 0, len(localDefaults))
	for _, maddr := range localDefaults {
		var (
			cleanup                       cleanupFunc
			unixSocketPath, hadUnixSocket = maybeGetUnixSocketPath(maddr)
		)
		if hadUnixSocket {
			// TODO: switch this back to regular Stat when this is merged
			// https://go-review.googlesource.com/c/go/+/338069/
			if _, err = os.Lstat(unixSocketPath); err == nil {
				return nil, nil, fmt.Errorf("socket file already exists: \"%s\"", unixSocketPath)
			}
			// If it contains a Unix socket, make the parent directory for it
			// and allow it to be deleted when the caller is done with it.
			parent := filepath.Dir(unixSocketPath)
			if err = os.MkdirAll(parent, 0o775); err != nil {
				return nil, nil, err
			}
			cleanup = func() error { return os.Remove(parent) }
		}

		// NOTE: While we're able to utilize/return multiple listeners here
		// we only want the first one we can actually acquire.
		listeners, err := listen(maddr)
		if err == nil {
			return listeners, cleanup, nil
		}
		tried = append(tried, attempted{
			Multiaddr: maddr,
			error:     err,
		})
		if cleanup != nil {
			if cErr := cleanup(); cErr != nil {
				return nil, nil, fmt.Errorf("%w - could not cleanup: %s", err, cErr)
			}
		}
	}

	// TODO: sloppy
	{
		err := fmt.Errorf("could not listen on any sockets")
		for _, attempt := range tried {
			err = fmt.Errorf("%w - \"%v\":\"%s\"", err, attempt.Multiaddr, attempt.error)
		}
		return nil, nil, err
	}
}

// maybeGetUnixSocketPath returns the path
// of the first Unix domain socket within the multiaddr (if any).
func maybeGetUnixSocketPath(ma multiaddr.Multiaddr) (target string, hadUnixComponent bool) {
	multiaddr.ForEach(ma, func(comp multiaddr.Component) bool {
		if hadUnixComponent = comp.Protocol().Code == multiaddr.P_UNIX; hadUnixComponent {
			target = comp.Value()
			if runtime.GOOS == "windows" { // `/C:\path` -> `C:\path`
				target = strings.TrimPrefix(target, `/`)
			}
			return true
		}
		return false
	})
	return
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

func serveCmdsHTTP(ctx context.Context,
	cmdsRoot *cmds.Command, cmdsEnv cmds.Environment,
	listeners ...manet.Listener) (<-chan multiaddr.Multiaddr, <-chan error) {
	var (
		maddrs    = make(chan multiaddr.Multiaddr, len(listeners))
		serveErrs = make(chan error)
		serveWg   sync.WaitGroup
	)
	defer close(maddrs)

	for _, listener := range listeners {
		serverErrs := acceptCmdsHTTP(ctx, listener, cmdsRoot, cmdsEnv)
		maddrs <- listener.Multiaddr() // Tell the caller this server is ready.

		// Aggregate server-errors into serve-errors.
		serveWg.Add(1)
		go func() {
			defer serveWg.Done()
			for err := range serverErrs {
				err = fmt.Errorf("HTTP server error: %w", err)
				serveErrs <- err
			}
		}()
	}

	// Close serveErrs after all aggregate servers close.
	go func() { defer close(serveErrs); serveWg.Wait() }()

	return maddrs, serveErrs
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
		select {
		case err := <-serveErr:
			httpServerErrs <- err
		case <-ctx.Done():
			timeout, timeoutCancel := context.WithTimeout(context.Background(),
				stopGrace/2)
			defer timeoutCancel()
			if err := httpServer.Shutdown(timeout); err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					err = fmt.Errorf("could not shutdown server before timeout (%s): %w",
						timeout, err,
					)
				}
				httpServerErrs <- err
			}

			// Serve routine must return now.
			if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
				httpServerErrs <- err
			}
		}
	}()

	return httpServerErrs
}

// TODO: this should probably return a pkg specific error value. (ErrAutoShutdown)
// ^ opted for a done-chan instead, caller can dispatch whatever they want at callsite
// TODO: [Ame] English.
//
// signalIfNotBusy checks every interval to see if the service is busy.
// If it's not, the returned channel will be closed.
// Otherwise, the service will be checked again next interval.
// If a service error is encountered at any point, it will be sent on the error channel.
// And receiving from the signal channel will block indefinitely.
func signalIfNotBusy(ctx context.Context,
	checkInterval time.Duration, _ environment.Environment) (<-chan struct{}, <-chan error) {
	if checkInterval == 0 {
		return nil, nil
	}
	var (
		idleSignal = make(chan struct{})
		ipcErrs    = make(chan error)
		// NOTE [placeholder]: This build is never busy.
		// The ipc env should be used to query activity when implemented.
		checkIfBusy = func() (bool, error) { return false, nil }
	)
	go func() {
		stopTicker := time.NewTicker(checkInterval)
		defer stopTicker.Stop()
		defer close(ipcErrs)
		for {
			select {
			case <-stopTicker.C:
				busy, err := checkIfBusy()
				if err != nil {
					select {
					case ipcErrs <- err:
					case <-ctx.Done():
					}
					return
				}
				if !busy {
					select {
					case idleSignal <- struct{}{}:
						defer close(idleSignal)
					case <-ctx.Done():
					}
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return idleSignal, ipcErrs
}
