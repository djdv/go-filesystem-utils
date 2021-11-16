// Package daemon exposes the means to host a service instance
// and connect as a client.
package daemon

import (
	"context"
	"fmt"
	"os"

	serviceenv "github.com/djdv/go-filesystem-utils/cmd/environment/service"
	stopenv "github.com/djdv/go-filesystem-utils/cmd/environment/service/daemon/stop"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	cmds "github.com/ipfs/go-ipfs-cmds"
	manet "github.com/multiformats/go-multiaddr/net"
)

const Name = "daemon"

var Command = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Manages file system requests and instances.",
	},
	NoRemote: true,
	Run:      daemonRun,
	Options:  parameters.CmdsOptionsFrom((*Settings)(nil)),
	Encoders: formats.CmdsEncoders,
	Type:     Response{},
	Subcommands: map[string]*cmds.Command{
		stop.Name: stop.Command,
	},
}

// CmdsPath returns the leading parameters
// to invoke the daemon's `Run` method from `main`.
func CmdsPath() []string { return []string{"service", "daemon"} }

func daemonRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	fmt.Println("daemon run enter")
	defer fmt.Println("daemon run exit")

	ctx, cancel := context.WithCancel(request.Context)
	defer cancel()

	settings, serviceEnv, err := parseAndEmitStarting(ctx, emitter, request, env)
	if err != nil {
		fmt.Println("test -2:", err)
		return err
	}

	// FIXME: stopping early - context needs to be changed.
	stopReasons, errs, err := startAllListeners(ctx,
		settings, serviceEnv, emitter, request)
	if err != nil {
		fmt.Println("test -1:", err)
		return err
	}
	cancelWithError := func(err error) error {
		cancel()
		for e := range errs {
			fmt.Println("test cancel e:", e)
			if err == nil {
				err = e
			} else {
				err = fmt.Errorf("%w\n\t%s", err, e)
			}
		}
		return err
	}

	fmt.Println("test 0")
	select {
	case err, ok := <-errs:
		// If we have immediate startup errors,
		// bail out.
		if !ok {
			// FIXME: should this break or panic?
			// We should close if no servers were started yet.
			break
			// fmt.Println("test panic⚠️")
			// panic("errs should not be closed yet")
		}
		// return cancelWithError(err)
		err = cancelWithError(err)
		fmt.Println("test 0 return:", err)
		return err
	default:
		// Otherwise just continue starting.
		fmt.Println("test 0 default")
	}

	fmt.Println("test 1")
	if err := emitReady(emitter); err != nil {
		// return cancelWithError(err)
		// DBG:
		err = cancelWithError(err)
		fmt.Println("test 1e:", err)
		return err
	}
	fmt.Println("test 2")
	// If we're a child process of the launcher;
	// start discarding stdio.
	/*
		if err := maybeDisableStdio(); err != nil {
			return err
		}
	*/

	{
		var err error
		select {
		case reason, ok := <-stopReasons:
			if ok {
				fmt.Println("test 3 A:", reason)
				err = emitStopping(emitter, reason)
			}
		case runErr, ok := <-errs:
			if ok {
				err = runErr
				fmt.Println("test 3 B:", err, ok)
				emitErr := emitStopping(emitter, stopenv.Error)
				if emitErr != nil {
					err = fmt.Errorf("%w\n\t%s", err, emitErr)
				}
			}
		}
		err = cancelWithError(err)
		fmt.Println("test 4:", err)
		return err
		// return cancelWithError(nil)
	}
}

func parseAndEmitStarting(ctx context.Context,
	emitter cmds.ResponseEmitter, request *cmds.Request,
	env cmds.Environment) (*Settings, serviceenv.Environment, error) {
	settings, serviceEnv, err := parseCmdsEnv(ctx, request, env)
	if err != nil {
		return nil, nil, err
	}
	if err := emitStarting(emitter); err != nil {
		return nil, nil, err
	}
	return settings, serviceEnv, nil
}

func parseCmdsEnv(ctx context.Context, request *cmds.Request,
	env cmds.Environment) (*Settings, serviceenv.Environment, error) {
	settings, err := parseSettings(ctx, request)
	if err != nil {
		return nil, nil, err
	}
	serviceEnv, err := serviceenv.Assert(env)
	if err != nil {
		return nil, nil, err
	}

	return settings, serviceEnv, nil
}

func startAllListeners(ctx context.Context,
	settings *Settings, serviceEnv serviceenv.Environment,
	emitter cmds.ResponseEmitter, request *cmds.Request) (<-chan stopenv.Reason,
	<-chan error, error) {

	stopper, stopReasons, err := setupStopper(ctx, request, emitter, serviceEnv)
	if err != nil {
		return nil, nil, err
	}

	type listenerConstructorFunc func() (<-chan error, error)
	var (
		errs <-chan error

		exitCheckInterval = settings.AutoExitInterval
		exitWhenIdle      = exitCheckInterval != 0

		listenerConstructors = []listenerConstructorFunc{
			func() (<-chan error, error) {
				return listenForGoSignals(ctx, emitter, request, stopper)
			},
			func() (<-chan error, error) {
				return listenAndServeCmdsHTTP(ctx, emitter, request, settings, serviceEnv)
			},
			func() (<-chan error, error) {
				if !exitWhenIdle {
					return nil, nil
				}
				return listenForIdleEvent(ctx, emitter, stopper, exitCheckInterval)
			},
		}
	)
	for _, listenerConstructor := range listenerConstructors {
		subErrs, err := listenerConstructor()
		if err != nil {
			return nil, nil, err
		}
		if subErrs == nil {
			continue
		}
		if errs == nil {
			errs = subErrs
		} else {
			errs = mergeErrs(errs, subErrs)
		}
	}
	return stopReasons, errs, nil
}

// merge error channels in any order.
func mergeErrs(car, cdr <-chan error) <-chan error {
	combined := make(chan error, cap(car)+cap(cdr))
	go func() {
		defer close(combined)
		for car != nil || cdr != nil {
			select {
			case err, ok := <-car:
				if !ok {
					car = nil
					continue
				}
				combined <- err
			case err, ok := <-cdr:
				if !ok {
					cdr = nil
					continue
				}
				combined <- err
			}
		}
	}()
	return combined
}

// join error channels in sequential order.
func joinErrs(car, cdr <-chan error) <-chan error {
	combined := make(chan error, cap(car)+cap(cdr))
	go func() {
		defer close(combined)
		for v := range car {
			combined <- v
		}
		for v := range cdr {
			combined <- v
		}
	}()
	return combined
}

func maybeDisableStdio() error {
	stdinStat, err := os.Stdin.Stat()
	if err != nil {
		return err
	}

	// TODO: receive the PID in the $ENV or something.
	// If we have it, check that we're a child of that process.
	// Otherwise, we're in a pipeline. I.e. we should keep stdout open.
	// The launcher(parent process),
	// will expect us to close our end of `pipe()`, before it releases us.
	// For now, all pipes will get messages up to `Ready` only.
	attachedToPipe := stdinStat.Mode().Type()&os.ModeNamedPipe != 0

	if attachedToPipe {
		// Close stdio and reopen as discard devices.
		// I.e. Emit's go nowhere.
		return disableStdio()
	}
	return nil
}

func disableStdio() error {
	for fdIndex, streamPtr := range []*os.File{
		os.Stdin,
		os.Stdout,
		os.Stderr,
	} {
		var flags int
		if fdIndex == 0 {
			flags = os.O_RDONLY
		} else {
			flags = os.O_WRONLY
		}

		if err := streamPtr.Close(); err != nil {
			return err
		}
		discard, err := os.OpenFile(os.DevNull, flags, 0)
		if err != nil {
			return err
		}

		*streamPtr = *discard
	}
	return nil
}

func listenAndServeCmdsHTTP(ctx context.Context,
	emitter cmds.ResponseEmitter, request *cmds.Request,
	settings *Settings, serviceEnv serviceenv.Environment) (<-chan error, error) {
	// FIXME: when we pull from listeners, if we're canceled,
	// close listener before using it for a new http server.
	var (
		serverRoot = allowRemoteAccess(
			request.Root,
			request.Path,
		)
		listenerResults = getListeners(ctx, request, settings.ServiceMaddrs...)
		listeners, errs = validateListeners(listenerResults)
		serverErrs      = serveCmdsHTTP(ctx, serverRoot, serviceEnv, listeners)
	)

	// TODO: this should be this functions return
	// return joinErrs(listenerErrs, serverErrs)

	defer close(errs)
	for result := range listenerResults {
		if err := result.error; err != nil {
			errs <- err
			continue
		}
		listener := result.Listener
		fmt.Println("👂 DBG: got listener:", listener.Multiaddr())
		if ctx.Err() != nil {
			fmt.Println("DBG: http: canceled before serve")
			err := listener.Close()
			return errs, err
		}
		// TODO stream to emitter? * ->
		if err := emitMaddrListener(emitter, listener.Multiaddr()); err != nil {
			fmt.Println("DBG: http: emit:", err)
			return errs, err
		}

		serveErrs := serveCmdsHTTP(ctx, serverRoot, serviceEnv, listeners)
		if errs == nil {
			errs = serveErrs
		} else {
			errs = mergeErrs(errs, serveErrs)
		}
	}
	return errs, nil
}

func validateListeners(ctx context.Context,
	results <-chan listenResult) (<-chan manet.Listener, <-chan error) {
	var (
		listeners = make(chan manet.Listener)
		errs      = make(chan error)
	)
	go func() {
		defer close(listeners)
		defer close(errs)
		for listener := range results {
			if err := listener.error; err != nil {
				select {
				case errs <- err:
					continue
				case <-ctx.Done():
					return
				}
			}
			select {
			case listeners <- listener.Listener:
			case <-ctx.Done():
				return
			}
		}
	}()
	return listeners, errs
}
