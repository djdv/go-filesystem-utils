// Package daemon exposes the means to host a service instance
// and connect as a client.
package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	serviceenv "github.com/djdv/go-filesystem-utils/cmd/environment/service"
	stopenv "github.com/djdv/go-filesystem-utils/cmd/environment/service/daemon/stop"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	cmds "github.com/ipfs/go-ipfs-cmds"
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

	settings, serviceEnv, err := parseAndEmitStart(ctx, emitter, request, env)
	if err != nil {
		return err
	}

	stopReasons, listenerCancel,
		errs, err := startAllListeners(ctx,
		request, emitter,
		settings, serviceEnv)
	if err != nil {
		return err
	}
	defer listenerCancel()

	if err := emitReady(emitter); err != nil {
		return err
	}
	// If we're a child process of the launcher;
	// start discarding stdio.
	if err := maybeDisableStdio(); err != nil {
		return err
	}

	select {
	case reason := <-stopReasons:
		err = emitStopping(emitter, reason)
	case err = <-errs:
		emitErr := emitStopping(emitter, stopenv.Error)
		if emitErr != nil {
			err = fmt.Errorf("%w - %s", err, emitErr)
		}
	}
	listenerCancel()

	for e := range errs {
		if err == nil {
			err = e
		} else {
			err = fmt.Errorf("%w - %s", err, e)
		}
	}

	return err
}

func parseAndEmitStart(ctx context.Context, emitter cmds.ResponseEmitter, request *cmds.Request,
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
	request *cmds.Request, emitter cmds.ResponseEmitter,
	settings *Settings, serviceEnv serviceenv.Environment) (<-chan stopenv.Reason,
	context.CancelFunc, <-chan error, error) {

	stopper, stopReasons, stopCancel,
		err := startStopperListener(emitter, request, serviceEnv)
	if err != nil {
		return nil, nil, nil, err
	}

	signalErrs, signalCancel, err := startSignalListeners(ctx,
		emitter, request,
		stopper)
	if err != nil {
		return nil, nil, nil, err
	}

	serverErrs, serverCancel, err := startServerListeners(ctx,
		settings, emitter,
		request, serviceEnv)
	if err != nil {
		return nil, nil, nil, err
	}

	var (
		errs = mergeErrs(
			signalErrs,
			serverErrs,
		)
		cancelers = []context.CancelFunc{
			stopCancel,
			signalCancel,
			serverCancel,
		}
		cancel context.CancelFunc = func() {
			for i := len(cancelers) - 1; i >= 0; i-- {
				cancelers[i]()
			}
		}

		exitCheckInterval = settings.AutoExitInterval
		exitWhenIdle      = exitCheckInterval != 0
	)
	if exitWhenIdle {
		busyCheckErrs, err := startIdleListener(ctx, emitter, stopper, exitCheckInterval)
		if err != nil {
			return nil, nil, nil, err
		}
		// TODO: dbg lint
		if errs == nil {
			fmt.Println("⚠️  errs1 was nil")
		}
		if busyCheckErrs == nil {
			fmt.Println("⚠️  busyCheckErrs was nil")
		}
		//

		errs = mergeErrs(errs, busyCheckErrs)
	}

	return stopReasons, cancel, errs, nil
}

func startStopperListener(emitter cmds.ResponseEmitter, request *cmds.Request,
	serviceEnv serviceenv.Environment) (stopenv.Environment, <-chan stopenv.Reason,
	context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(context.Background())
	stopper, stopReasons, err := setupStopper(ctx, serviceEnv)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	stopPath := append(request.Path, stop.Name)
	if err := emitStopListener(emitter, stopPath...); err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return stopper, stopReasons, cancel, nil
}

func startSignalListeners(ctx context.Context, emitter cmds.ResponseEmitter,
	request *cmds.Request, stopper stopenv.Environment) (<-chan error, context.CancelFunc, error) {
	var (
		cancel       context.CancelFunc
		notifySignal = os.Interrupt
	)
	ctx, cancel = context.WithCancel(ctx)

	osErrs, err := startOSSignalListener(ctx, emitter,
		stopper, stopenv.Canceled,
		notifySignal,
	)
	if err != nil {
		cancel()
		return nil, nil, err
	}

	requestErrs, err := startRequestListener(ctx, emitter, request, stopper, stopenv.Canceled)
	if err != nil {
		cancel()
		return nil, nil, err
	}

	return mergeErrs(osErrs, requestErrs), cancel, nil
}

func startOSSignalListener(ctx context.Context, emitter cmds.ResponseEmitter,
	stopper stopenv.Environment, stopReason stopenv.Reason,
	notifySignal os.Signal) (<-chan error, error) {
	if err := emitSignalListener(emitter, notifySignal); err != nil {
		return nil, err
	}
	return stopOnSignal(ctx, stopper, stopReason, notifySignal), nil
}

// TODO: review; jank?
func startRequestListener(ctx context.Context, emitter cmds.ResponseEmitter,
	request *cmds.Request, stopper stopenv.Environment,
	stopReason stopenv.Reason) (<-chan error, error) {
	if err := emitCmdsListener(emitter); err != nil {
		return nil, err
	}

	var (
		triggerCtx = request.Context
		errs       = make(chan error, 1)
	)
	go func() {
		defer close(errs)
		select {
		case <-triggerCtx.Done():
			if err := stopper.Stop(stopReason); err != nil {
				errs <- err
			} else {
				errs <- triggerCtx.Err()
			}
		case <-ctx.Done():
		}
	}()

	return errs, nil
}

func startServerListeners(ctx context.Context, settings *Settings,
	emitter cmds.ResponseEmitter, request *cmds.Request,
	serviceEnv serviceenv.Environment) (<-chan error, context.CancelFunc, error) {
	listeners, listenerErrs, err := getListeners(ctx, request, settings.ServiceMaddrs...)
	if err != nil {
		return nil, nil, err
	}
	// Duplicate values into cmds emitter.
	// Relay back to us.
	var emitterErrs <-chan error
	listeners, emitterErrs = emitAndRelayListeners(emitter, listeners)

	var (
		serverCtx, serverCancel = context.WithCancel(context.Background())
		serverRoot              = allowRemoteAccess(
			request.Root,
			request.Path,
		)
		serverErrs = setupCmdsHTTP(serverCtx,
			serverRoot,
			serviceEnv,
			listeners,
		)
		errs = mergeErrs(
			listenerErrs,
			emitterErrs,
			serverErrs)
	)

	// TODO: dbg lint
	if errs == nil {
		fmt.Println("⚠️ errs  2 was nil")
	}
	if listenerErrs == nil {
		fmt.Println("⚠️  listenerErrs was nil")
	}
	if emitterErrs == nil {
		fmt.Println("⚠️  emitterErrs was nil")
	}
	if serverErrs == nil {
		fmt.Println("⚠️  serverErrs 3 was nil")
	}
	//

	return errs, serverCancel, nil
}

func startIdleListener(ctx context.Context, emitter cmds.ResponseEmitter,
	stopper stopenv.Environment, interval time.Duration) (<-chan error, error) {
	// NOTE [placeholder]: This build is never busy.
	// The ipc env should be used to query activity when implemented.
	checkIfBusy := func() (bool, error) {
		return false, nil
	}
	if err := emitTickerListener(emitter,
		interval, "is-service-idle-every"); err != nil {
		return nil, err
	}
	return stopOnIdle(ctx, stopper, interval, checkIfBusy), nil
}

func mergeErrs(sources ...<-chan error) <-chan error {
	if len(sources) == 1 {
		fmt.Println("mergeErrs ret:", sources[0])
		return sources[0]
	}

	type (
		source   = <-chan error
		sourceRw = chan error
	)
	_, dbgF, dbgL, ok := runtime.Caller(1)
	if !ok {
		panic("dbg runtime routine failed")
	}
	dbgF = filepath.Base(dbgF)
	var (
		mergedWg  sync.WaitGroup
		mergedCh  = make(sourceRw)
		mergeFrom = func(ch source) {
			for value := range ch {
				mergedCh <- value
			}
			mergedWg.Done()
		}
		mergeFromDbg = func(ch source) {
			fmt.Printf("reading: (%s:%d) %v\n",
				dbgF, dbgL, ch)
			chDbg := ch
			chStall := time.After(5 * time.Second)
			for ch != nil {
				select {
				case <-time.After(1 * time.Second):
					continue
				case <-chStall:
					fmt.Printf("stalling: (%s:%d) %v\n",
						dbgF, dbgL, ch)
				case value, ok := <-ch:
					if !ok {
						ch = nil
						continue
					}
					mergedCh <- value
				}
			}
			fmt.Printf("done reading: (%s:%d) %v\n",
				dbgF, dbgL, chDbg)
			mergedWg.Done()
		}
	)

	mergedWg.Add(len(sources))
	go func() { mergedWg.Wait(); close(mergedCh) }()

	for _, source := range sources {
		go mergeFrom(source)
		// _ = mergeFrom
		// go mergeFromDbg(source)
		_ = mergeFromDbg
	}

	return mergedCh
}

type cleanupFunc func() error

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
		if err := streamPtr.Close(); err != nil {
			return err
		}
		var flags int
		if fdIndex == 0 {
			flags = os.O_RDONLY
		} else {
			flags = os.O_WRONLY
		}
		discard, err := os.OpenFile(os.DevNull, flags, 0)
		if err != nil {
			return err
		}
		*streamPtr = *discard
	}
	return nil
}
