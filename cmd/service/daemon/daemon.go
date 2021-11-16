// Package daemon exposes the means to host a service instance
// and connect as a client.
package daemon

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	serviceenv "github.com/djdv/go-filesystem-utils/cmd/environment/service"
	stopenv "github.com/djdv/go-filesystem-utils/cmd/environment/service/daemon/stop"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
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
	ctx, cancel := context.WithCancel(request.Context)
	defer cancel()

	settings, serviceEnv, err := parseAndEmitStarting(ctx, emitter, request, env)
	if err != nil {
		return err
	}
	stopper, stopReasons, err := setupStopper(ctx, request, emitter, serviceEnv)
	if err != nil {
		return err
	}

	errs, err := setupAllListeners(ctx, emitter, request, settings, stopper, serviceEnv)
	if err != nil {
		return err
	}
	cancelWithError := func(err error) error {
		cancel()
		for e := range errs {
			if err == nil {
				err = e
			} else {
				err = fmt.Errorf("%w\n\t%s", err, e)
			}
		}
		return err
	}

	select {
	case err, ok := <-errs:
		// If we have immediate startup errors,
		// bail out.
		if !ok {
			fmt.Println("errs in run not okay ❔")
			// FIXME: should this break or panic?
			// We should close if no servers were started yet.
			break
			// fmt.Println("test panic⚠️")
			// panic("errs should not be closed yet")
		}
		return cancelWithError(err)
	default:
		// Otherwise just continue starting.
	}

	// If we're a child process of the launcher;
	// start discarding stdio.
	if err := maybeDisableStdio(); err != nil {
		return err
	}

	{
		var err error
		select {
		case reason, ok := <-stopReasons:
			if ok {
				err = emitStopping(emitter, reason)
			}
		case runErr, ok := <-errs:
			if ok {
				err = runErr
				emitErr := emitStopping(emitter, stopenv.Error)
				if emitErr != nil {
					err = fmt.Errorf("%w\n\t%s", err, emitErr)
				}
			}
		}
		return cancelWithError(err)
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

// TODO: setup HTTP first, start setting up extras, wait for HTTP, emit Ready
// return error bridge for both errs?
// ^ just make the http constructor block internally for now?
// returns nil when all listeners are serving, relays listeners as they're listening though.
//^^ We need to refactor from left -> right
// we shouldn't be passing in emit, we should be looping over a channel of results
// and emitting or returning conditionally. Like is being done on the rightmost code paths.
func setupAllListeners(ctx context.Context,
	emitter cmds.ResponseEmitter, request *cmds.Request,
	settings *Settings, stopper stopenv.Environment,
	serviceEnv serviceenv.Environment) (<-chan error, error) {

	initErrs, err := setupInitListeners(ctx, emitter, request, stopper)
	if err != nil {
		return nil, err
	}

	var (
		serveResults = listenAndServeCmdsHTTP(ctx, request, settings, serviceEnv)
		// errs         = make(chan (<-chan error))
		errs = make(chan (<-chan error), 10)
	)
	errs <- initErrs
	// TODO: this in a goroutine
	// but also it has its own error channel which is closed after all servers are okay.
	// This will be our "wait for server init" condition
	// serverInitErr = chan errs
	// go for {
	// server okay? keep going
	// server or emit not okay? close all listeners; drain any bridges errs; return
	// }
	todoName := make(chan error)
	go func() {
		defer close(todoName)
		var (
			serveErr     error
			wrapServeErr = func(err error) error {
				if serveErr == nil {
					serveErr = err
				} else {
					serveErr = fmt.Errorf("%w\n\t%s", serveErr, err)
				}
				return serveErr
			}
		)
		for result := range serveResults {
			fmt.Printf("DBG l&s 1:%#v", result)
			if err := result.error; err != nil {
				fmt.Println("DBG l&s 1e:", err)
				serveErr = wrapServeErr(err)
				continue
			}
			if err := emitMaddrListener(emitter, result.serverAddress); err != nil {
				fmt.Println("DBG l&s 2e:", err)
				serveErr = wrapServeErr(err)
				continue
			}
			// TODO:
			// These have to go back to the caller.
			// We could queue them, if error, drain.
			// Otherwise, merge and return with nil err
			//_ = result.serverErrs
			errs <- result.serverErrs
		}
		if serveErr != nil {
			todoName <- serveErr
		}
	}()
	// FIXME: this is just for debugging, we need to flatten this channel into a single err
	// and likely have to wait/drain existing error bridges after a cancel, unless we can return the bridge with an error.
	// or vice versa (prepend the err to the bridge and expect the caller to select-default check it)
	if err := <-todoName; err != nil {
		return nil, err
	}

	// TODO We need to sync on listenerErrs here
	// if the servers didn't start, bail out.
	//
	// TODO: emit "Ready" here
	// FIXME: we need to sync with http server chan finish
	// then emit ready, we can emit idle before or after, doesn't matter.
	// If not done, parent processes will see "starting, ready" == no listeners
	// vs "starting, maddr1,maddr2,ready" == []m1,m2
	//
	// do initSequence startListeners([]constructors);
	// emit(ready); startListeners([]extraConsturctors
	// with bridge in between for values.
	//
	// init -> readChan -> emit ready -> extraInit -> extra emit
	//         ⌞       here         ⌟
	if err := emitReady(emitter); err != nil {
		return nil, err
	}

	extraErrs, err := maybeSetupExtraListeners(ctx, emitter, settings, stopper)
	if err != nil {
		// return fanInErrs(initErrs, serverErrs, extraErrs), err
		return nil, err
	}
	if extraErrs != nil {
		errs <- extraErrs
	}

	close(errs) // TODO: review async access
	// This hsould be less magic.
	// It works as is because we wait for init's routine to be done sending
	// but should do this properly.

	return bridgeErrs(errs), nil
}

/*
func setupAllListeners(ctx context.Context,
	emitter cmds.ResponseEmitter, request *cmds.Request,
	settings *Settings, stopper stopenv.Environment,
	serviceEnv serviceenv.Environment) (<-chan error, error) {
	errs := make(chan (<-chan error), 2)
	defer close(errs)

	initErrs, err := setupInitListeners(ctx,
		emitter, request,
		settings, stopper, serviceEnv,
	)
	if err != nil {
		return nil, err
	}
	errs <- initErrs

	// TODO We need to sync on listenerErrs here
	// if the servers didn't start, bail out.
	//
	// TODO: emit "Ready" here
	// FIXME: we need to sync with http server chan finish
	// then emit ready, we can emit idle before or after, doesn't matter.
	// If not done, parent processes will see "starting, ready" == no listeners
	// vs "starting, maddr1,maddr2,ready" == []m1,m2
	//
	// do initSequence startListeners([]constructors);
	// emit(ready); startListeners([]extraConsturctors
	// with bridge in between for values.
	//
	// init -> readChan -> emit ready -> extraInit -> extra emit
	//         ⌞       here         ⌟
	if err := emitReady(emitter); err != nil {
		return bridgeErrs(errs), err
	}

	extraErrs, err := maybeSetupExtraListeners(ctx, emitter, settings, stopper)
	if err != nil {
		return bridgeErrs(errs), err
	}
	if extraErrs != nil {
		errs <- extraErrs
	}

	return bridgeErrs(errs), nil
}
*/

// TODO: try to abstract the emissions better.
// Maybe some result type with like emitFunc() error that wraps the input to re.Emit?
// results <- result.func = { return emitSignal(...)}
func setupInitListeners(ctx context.Context,
	emitter cmds.ResponseEmitter, request *cmds.Request,
	stopper stopenv.Environment) (<-chan error, error) {

	const reason = stopenv.Canceled
	var (
		notifySignal = os.Interrupt
		osErrs       = stopOnSignal(ctx, stopper, reason, notifySignal)
	)
	if err := emitSignalListener(emitter, notifySignal); err != nil {
		return nil, err
	}

	cmdsErrs := listenForRequestCancel(ctx, request, stopper, stopenv.Canceled)
	if err := emitCmdsListener(emitter); err != nil {
		return nil, err
	}

	return joinErrs(osErrs, cmdsErrs), nil
}

/*
func setupInitListeners(ctx context.Context,
	request *cmds.Request, settings *Settings,
	stopper stopenv.Environment, serviceEnv serviceenv.Environment) (<-chan error, error) {
	return setupListeners(
		func() (<-chan error, error) {
			return stopOnGoSignals(ctx, request, stopper)
		},
		func() (<-chan error, error) {
			return listenAndServeCmdsHTTP(ctx, request, settings, serviceEnv), nil
		},
	)
}
*/

func maybeSetupExtraListeners(ctx context.Context,
	emitter cmds.ResponseEmitter, settings *Settings,
	stopper stopenv.Environment) (<-chan error, error) {
	var (
		exitCheckInterval = settings.AutoExitInterval
		exitWhenIdle      = exitCheckInterval != 0
	)
	return setupListeners(
		func() (<-chan error, error) {
			if !exitWhenIdle {
				return nil, nil
			}
			return listenForIdleEvent(ctx, emitter, stopper, exitCheckInterval)
		},
	)
}

type listenerConstructorFunc func() (<-chan error, error)

func setupListeners(listenerConstructors ...listenerConstructorFunc) (<-chan error, error) {
	errs := make(chan (<-chan error), len(listenerConstructors))
	defer close(errs)
	for _, constructor := range listenerConstructors {
		subErrs, err := constructor()
		if err != nil {
			return nil, err
		}
		if subErrs != nil {
			errs <- subErrs
		}
	}
	return bridgeErrs(errs), nil
}

func bridgeErrs(input <-chan <-chan error,
) <-chan error {
	output := make(chan error)
	go func() {
		defer close(output)
		for errs := range input {
			for err := range errs {
				output <- err
			}
		}
	}()
	return output
}

func fanInErrs(sources ...<-chan error) <-chan error {
	type (
		source   = <-chan error
		sourceRw = chan error
	)
	var (
		mergedWg  sync.WaitGroup
		mergedCh  = make(sourceRw)
		mergeFrom = func(ch source) {
			for value := range ch {
				mergedCh <- value
			}
			mergedWg.Done()
		}
	)

	mergedWg.Add(len(sources))
	go func() { mergedWg.Wait(); close(mergedCh) }()

	for _, source := range sources {
		go mergeFrom(source)
	}

	return mergedCh
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

type serveResult struct {
	serverAddress multiaddr.Multiaddr
	serverErrs    <-chan error
	error
}

func listenAndServeCmdsHTTP(ctx context.Context, request *cmds.Request,
	settings *Settings, serviceEnv serviceenv.Environment) <-chan serveResult {
	const (
		errPrefix     = "listenAndServe"
		shutdownGrace = 30 * time.Second
	)
	var (
		listenResults = getListeners(ctx, request, settings.ServiceMaddrs...)
		serveResults  = make(chan serveResult, cap(listenResults))
		serverRoot    = allowRemoteAccess(
			request.Root,
			request.Path,
		)
	)
	go func() {
		// NOTE: All input listeners must be closed before we return.
		// I.e. don't close on cancel.
		defer close(serveResults)
		for result := range listenResults {
			if err := result.error; err != nil {
				err = fmt.Errorf("%s failed to listen: %w",
					errPrefix, err)
				serveResults <- serveResult{error: err}
				continue
			}
			listener := result.Listener

			if ctx.Err() != nil {
				if err := listener.Close(); err != nil {
					err = fmt.Errorf("%s failed to close listener: %w",
						errPrefix, err)
					serveResults <- serveResult{error: err}
				}
				continue
			}

			var (
				server    = httpServerFromCmds(serverRoot, serviceEnv)
				serveErrs = serveHTTP(ctx, listener, server, shutdownGrace)
			)
			serveResults <- serveResult{
				serverAddress: listener.Multiaddr(),
				serverErrs:    serveErrs,
			}
		}
	}()
	return serveResults
}

/*
func listenAndServeCmdsHTTP(ctx context.Context, request *cmds.Request,
	settings *Settings, serviceEnv serviceenv.Environment) <-chan ListenAndServeResult {
	const stopGrace = 30 * time.Second
	var (
		listenResults = getListeners(ctx, request, settings.ServiceMaddrs...)
		serveResults  = make(chan ListenAndServeResult, cap(listenResults))
		serverRoot    = allowRemoteAccess(
			request.Root,
			request.Path,
		)
	)
	go func() {
		// NOTE: All input listeners must be closed before we return.
		// I.e. don't close on cancel.
		defer close(serveResults)
		for listener := range listenResults {
			if err := listener.error; err != nil {
				serveResults <- ListenAndServeResult{error: err}
				continue
			}
			if ctx.Err() != nil {
				if err := listener.Close(); err != nil {
					serveResults <- ListenAndServeResult{error: err}
				}
				continue
			}
			serveResults <- ListenAndServeResult{
				serverAddress: listener.Multiaddr(),
				serverErrs:    serveCmdsHTTP(ctx, listener, serverRoot, serviceEnv),
			}
		}
	}()
	return serveResults
}
*/

/*
func listenAndServeCmdsHTTP(srvCtx context.Context, request *cmds.Request,
	settings *Settings, serviceEnv serviceenv.Environment) (<-chan multiaddr.Multiaddr, <-chan error) {
	var (
		listenerResults = getListeners(srvCtx, request, settings.ServiceMaddrs...)
		listenerErrs    = make(chan error, cap(listenerResults))

		servingMaddrs = make(chan multiaddr.Multiaddr, cap(listenerResults))
		serverRoot    = allowRemoteAccess(
			request.Root,
			request.Path,
		)

		closeErrs = make(chan error, cap(listenerResults))
		errs      = make(chan (<-chan error))
	)

	go func() {
		defer close(servingMaddrs)
		defer close(closeErrs)
		defer close(errs)
		errs <- listenerErrs

		for listener := range listenerResults {
			if srvCtx.Err() != nil {
				if err := listener.Close(); err != nil {
					closeErrs <- err
				}
				continue
			}
			if listener.error != nil {
				continue
			}

			errs <- serveCmdsHTTP(srvCtx, listener, serverRoot, serviceEnv)
			servingMaddrs <- listener.Multiaddr()
		}
	}()

	return servingMaddrs, bridgeErrs(errs)
}
*/

/*
func listenAndServeCmdsHTTP(ctx context.Context,
	emitter cmds.ResponseEmitter, request *cmds.Request,
	settings *Settings, serviceEnv serviceenv.Environment) <-chan error {
	var (
		listenerResults         = getListeners(ctx, request, settings.ServiceMaddrs...)
		listeners, listenerErrs = validateListeners(listenerResults)

		emitErrs            = make(chan error)
		listenerRelay       = make(chan manet.Listener, cap(listeners))
		emitCtx, emitCancel = context.WithCancel(ctx)

		serverRoot = allowRemoteAccess(
			request.Root,
			request.Path,
		)
		serverErrs = httpServeFromListeners(emitCtx, serverRoot, serviceEnv, listenerRelay)
	)
	go func() {
		defer close(listenerRelay)
		defer close(emitErrs)
		for listener := range listeners {
			if ctx.Err() != nil {
				fmt.Println("💃")
				if err := listener.Close(); err != nil {
					emitCancel() // XXX: rethink context handling
					emitErrs <- err
				}
				return
			}
			if err := emitMaddrListener(emitter, listener.Multiaddr()); err != nil {
				emitCancel() // XXX: rethink context handling
				emitErrs <- err
				return
			}
			listenerRelay <- listener
		}
	}()

	// TODO: make this merge errors?
	errs := make(chan (<-chan error), 3)
	errs <- listenerErrs
	errs <- emitErrs
	errs <- serverErrs
	close(errs)

	return bridgeErrs(errs)
}
*/

/*
func splitListenResults(results <-chan listenResult) (<-chan manet.Listener, <-chan error) {
	var (
		listeners = make(chan manet.Listener, cap(results))
		errs      = make(chan error)
	)
	go func() {
		defer close(listeners)
		defer close(errs)
		for listener := range results {
			if err := listener.error; err != nil {
				errs <- err
				continue
			}
			listeners <- listener.Listener
		}
	}()
	return listeners, errs
}
*/
