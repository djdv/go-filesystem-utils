package daemon

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment"
	"github.com/djdv/go-filesystem-utils/cmd/ipc/environment/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	cmds "github.com/ipfs/go-ipfs-cmds"
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

func clientRoot() *cmds.Command {
	return &cmds.Command{
		Options: fscmds.RootOptions(),
		Helptext: cmds.HelpText{
			Tagline: "File system service client.",
		},
		Subcommands: map[string]*cmds.Command{
			"service": {
				// TODO : add stub info
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

func maybeAppendError(origErr, newErr error) error {
	switch {
	case origErr == nil:
		return newErr
	case newErr == nil:
		return origErr
	default:
		return fmt.Errorf("%w - %s", origErr, newErr)
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

	var (
		unwind  []func() error
		cleanup = func() error {
			var err error
			for i := len(unwind) - 1; i != -1; i-- {
				err = maybeAppendError(err, unwind[i]())
			}
			return err
		}
	)

	// Setup and handle daemonEnv.Stop().
	daemonEnv := ipcEnv.Daemon()
	stopReasons, err := daemonEnv.InitializeStop(ctx)
	if err != nil {
		return err
	}
	stopCtx, stopErrs := watchForStopReasons(ctx, stopReasons, emitter)
	unwind = append(unwind, errChanToUnwindFunc(stopErrs))

	// Call daemonEnv.Stop() when this context is canceled.
	// (But not if we already stopped for some other reason)
	var (
		stopTriggerCtx, stopTriggerCancel = signal.NotifyContext(ctx, os.Interrupt)
		ctxStopperErrs                    = stopOnContext(stopCtx, stopTriggerCtx, daemonEnv)
	)
	defer stopTriggerCancel()
	unwind = append(unwind, errChanToUnwindFunc(ctxStopperErrs))

	// Start listening on sockets.
	listeners, listenerCleanup, err := getListeners(request.Command.Extra, settings.ServiceMaddrs...)
	if err != nil {
		return err
	}
	if listenerCleanup != nil {
		unwind = append(unwind, listenerCleanup)
	}

	var (
		serverCtx, serverCancel  = context.WithCancel(stopTriggerCtx)
		clientRoot               = clientRoot()
		serverMaddrs, serverErrs = serveCmdsHTTP(serverCtx, clientRoot, env, listeners...)
	)
	unwind = append(unwind, func() error {
		// Cancel the server(s) and wait for listener(s) to close
		// (by blocking on their error channel)
		serverCancel()
		return errChanToUnwindFunc(serverErrs)()
	})

	for listenerMaddr := range serverMaddrs {
		if err := emitter.Emit(&daemon.Response{
			Status:        daemon.Ready,
			ListenerMaddr: &formats.Multiaddr{Interface: listenerMaddr},
		}); err != nil {
			return maybeAppendError(err, cleanup())
		}
	}

	if err := emitter.Emit(&daemon.Response{
		Status: daemon.Ready,
	}); err != nil {
		return maybeAppendError(err, cleanup())
	}

	if busyCheckInterval := settings.AutoExitInterval; busyCheckInterval != 0 {
		if err := emitter.Emit(&daemon.Response{
			Info: fmt.Sprintf("Requested to stop if not busy every %s",
				busyCheckInterval),
		}); err != nil {
			return maybeAppendError(err, cleanup())
		}
		idleErrs := stopIfNotBusy(ctx, busyCheckInterval, ipcEnv)
		unwind = append(unwind, errChanToUnwindFunc(idleErrs))
	}

	<-stopCtx.Done()

	return cleanup()
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
		manetListeners, ok := cmdsListeners.([]manet.Listener)
		if !ok {
			err := fmt.Errorf("Command.Extra value has wrong type"+
				"expected %T"+
				"got: %T",
				manetListeners,
				cmdsListeners,
			)
			return nil, nil, err
		}
		listeners = manetListeners
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

func watchForStopReasons(ctx context.Context, stopReasons <-chan daemon.StopReason,
	emitter cmds.ResponseEmitter) (context.Context, <-chan error) {
	errs := make(chan error)
	stopCtx, stopCancel := context.WithCancel(context.Background())
	go func() {
		defer close(errs)
		defer stopCancel()
		select {
		case reason := <-stopReasons:
			if err := emitter.Emit(&daemon.Response{
				Status:     daemon.Stopping,
				StopReason: reason,
			}); err != nil {
				errs <- err
			}
		case <-ctx.Done():
		}
	}()
	return stopCtx, errs
}

func stopOnContext(ctx, triggerCtx context.Context, daemonEnv daemon.Environment) <-chan error {
	errs := make(chan error)
	go func() {
		defer close(errs)
		select {
		case <-triggerCtx.Done():
		case <-ctx.Done():
			return
		}
		if stopErr := daemonEnv.Stop(daemon.RequestCanceled); stopErr != nil {
			select {
			case errs <- stopErr:
			case <-ctx.Done():
			}
		}
	}()
	return errs
}

func errChanToUnwindFunc(errs <-chan error) func() error {
	return func() error {
		var err error
		for e := range errs {
			err = maybeAppendError(err, e)
		}
		return err
	}
}
