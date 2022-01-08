// Package daemon exposes the means to host a service instance
// and connect as a client.
package daemon

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/djdv/go-filesystem-utils/cmd/environment"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	"github.com/djdv/go-filesystem-utils/filesystem"
	fscmds "github.com/djdv/go-filesystem-utils/filesystem/cmds"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

type (
	stdioSignal = byte

	runEnv struct {
		cmds.ResponseEmitter
		*Settings
		environment.Environment
	}

	taskErr struct {
		foreground error
		background <-chan error
	}
)

const (
	// End Of Transmission `♦` may be sent to stdin.
	// Sender must close stdin after sending the signal.
	// Receiver will close stdout and stderr.
	ASCIIEOT stdioSignal = 0x4

	Name = "daemon"
)

var Command = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "Manages file system requests and instances.",
	},
	NoRemote: true,
	PreRun:   daemonPreRun,
	Run:      daemonRun,
	Options:  parameters.CmdsOptionsFrom((*Settings)(nil)),
	Encoders: fscmds.CmdsEncoders,
	Type:     Response{},
	Subcommands: map[string]*cmds.Command{
		stop.Name: stop.Command,
	},
}

// CmdsPath returns the leading parameters
// to invoke the daemon's `Run` method from `main`.
func CmdsPath() []string { return []string{"service", Name} }

func daemonPreRun(*cmds.Request, cmds.Environment) error {
	return filesystem.RegisterPathMultiaddr()
}

func daemonRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runEnv, stopReasons, setupErrs := setupRunEnv(ctx, request, emitter, env)
	if err := setupErrs.foreground; err != nil {
		return err
	}
	return serverRun(ctx, cancel, request, runEnv, stopReasons)
}

func parseInputs(ctx context.Context, request *cmds.Request,
	env cmds.Environment) (*Settings, environment.Environment, error) {
	settings, serviceEnv, err := parseCmds(ctx, request, env)
	if err != nil {
		return nil, nil, err
	}
	return settings, serviceEnv, nil
}

func parseCmds(ctx context.Context, request *cmds.Request,
	env cmds.Environment) (*Settings, environment.Environment, error) {
	settings, err := parseSettings(ctx, request)
	if err != nil {
		return nil, nil, err
	}
	serviceEnv, err := environment.Assert(env)
	if err != nil {
		return nil, nil, err
	}
	return settings, serviceEnv, nil
}

func setupRunEnv(ctx context.Context,
	request *cmds.Request, emitter cmds.ResponseEmitter,
	env cmds.Environment) (*runEnv, <-chan environment.Reason, taskErr) {
	settings, serviceEnv, err := parseInputs(ctx, request, env)
	if err != nil {
		return nil, nil, taskErr{foreground: err}
	}
	var (
		muEmitter, ioErrs = synchronizeWithStdio(
			emitter,
			os.Stdin, os.Stdout, os.Stderr,
		)
		runEnv = &runEnv{
			Settings:        settings,
			Environment:     serviceEnv,
			ResponseEmitter: muEmitter,
		}
	)
	if ioErrs.foreground != nil {
		return nil, nil, ioErrs
	}

	stopReasons, err := setupStopper(ctx, request, runEnv)
	if err != nil {
		errs := taskErr{foreground: err, background: ioErrs.background}
		return nil, nil, errs
	}

	if err := muEmitter.Emit(startingResponse()); err != nil {
		errs := taskErr{foreground: err, background: ioErrs.background}
		return nil, nil, errs
	}

	return runEnv, stopReasons, ioErrs
}

func serverRun(ctx context.Context, cancel context.CancelFunc,
	request *cmds.Request,
	runEnv *runEnv, stopReasons <-chan environment.Reason) error {
	var (
		taskErrs []<-chan error

		stderr    = os.Stderr
		checkTask = func(errs taskErr) error {
			if bgErrs := errs.background; bgErrs != nil {
				taskErrs = append(taskErrs, bgErrs)
			}
			if err := errs.foreground; err != nil {
				errs.background = flattenErrs(taskErrs...)
				return shutdownDaemon(ctx, cancel, stderr, errs)
			}
			return nil
		}
	)
	if err := checkTask(
		setupPrimaryStoppers(ctx, request, runEnv),
	); err != nil {
		return err
	}

	if err := checkTask(
		setupServers(ctx, request, runEnv),
	); err != nil {
		return err
	}

	if err := checkTask(
		setupSecondaryStoppers(ctx, runEnv),
	); err != nil {
		return err
	}
	// TODO: collapse above further

	errs := flattenErrs(taskErrs...)
	return shutdownDaemon(ctx, cancel, stderr, taskErr{
		foreground: waitForStopOrError(runEnv.ResponseEmitter, stopReasons, errs),
		background: errs,
	})
}

func setupPrimaryStoppers(ctx context.Context,
	request *cmds.Request, runEnv *runEnv) taskErr {
	const reason = environment.Canceled
	var (
		notifySignal = os.Interrupt
		stopper      = runEnv.Daemon().Stopper()
		signalErrs   = stopOnSignal(ctx, stopper, reason, notifySignal)
		errs         = taskErr{background: signalErrs}
	)
	if err := runEnv.Emit(signalListenerResponse(notifySignal)); err != nil {
		errs.foreground = err
		return errs
	}

	requestErrs := stopOnRequestCancel(ctx, request, stopper, environment.Canceled)
	errs.background = maybeMergeErrs(signalErrs, requestErrs)
	if err := runEnv.Emit(cmdsListenerResponse()); err != nil {
		errs.foreground = err
	}

	return errs
}

func setupServers(ctx context.Context,
	request *cmds.Request, runEnv *runEnv) taskErr {
	listenErrs := listenAndServe(ctx, request, runEnv)
	if listenErrs.foreground != nil {
		return listenErrs
	}
	if err := runEnv.Emit(readyResponse()); err != nil {
		return taskErr{foreground: err, background: listenErrs.background}
	}
	return listenErrs
}

func listenAndServe(ctx context.Context,
	request *cmds.Request, runEnv *runEnv) taskErr {
	return makeCmdsServers(ctx, request, runEnv)
}

func makeCmdsServers(ctx context.Context,
	request *cmds.Request, runEnv *runEnv) taskErr {
	var (
		errs               []error
		serverResults, err = listenAndServeCmdsHTTP(ctx, request, runEnv)
		allServerErrs      = make([]<-chan error, 0, cap(serverResults))
	)
	if err != nil {
		return taskErr{foreground: err}
	}
	for result := range serverResults {
		err := result.error
		if err == nil {
			serverErrs := result.serverErrs
			allServerErrs = append(allServerErrs, serverErrs)
			err = runEnv.Emit(maddrListenerResponse(result.serverAddress))
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return taskErr{
		foreground: flattenErr(errs...),
		background: mergeErrs(allServerErrs...),
	}
}

func setupSecondaryStoppers(ctx context.Context, runEnv *runEnv) taskErr {
	var (
		settings          = runEnv.Settings
		exitCheckInterval = settings.AutoExitInterval
		exitWhenIdle      = exitCheckInterval != 0
	)
	if !exitWhenIdle {
		return taskErr{}
	}
	var ()
	return stopOnIdleEvent(ctx, runEnv, exitCheckInterval)
}

func waitForStopOrError(emitter cmds.ResponseEmitter,
	reasons <-chan environment.Reason, errs <-chan error) error {
	select {
	case reason := <-reasons:
		return emitter.Emit(stoppingResponse(reason))
	case err := <-errs:
		return maybeWrapErr(err,
			emitter.Emit(stoppingResponse(environment.Error)),
		)
	}
}

func shutdownDaemon(ctx context.Context, cancel context.CancelFunc,
	stderr *os.File, errs taskErr) error {
	return handleStderr(stderr,
		stopDaemon(ctx, cancel, errs),
	)
}

func stopDaemon(ctx context.Context, cancel context.CancelFunc, errs taskErr) error {
	select {
	case <-ctx.Done():
	default:
		cancel()
	}
	err := errs.foreground
	if errs.background != nil {
		err = maybeWrapErr(err, combineErrs(errs.background))
	}
	return err
}

func flattenErr(errors ...error) (err error) {
	for _, e := range errors {
		err = maybeWrapErr(err, e)
	}
	return
}

func maybeWrapErr(car, cdr error) error {
	if car == nil {
		return cdr
	} else if cdr != nil {
		return fmt.Errorf("%w\n\t%s", car, cdr)
	}
	return car
}

func flattenErrs(chans ...<-chan error) (errs <-chan error) {
	for _, ch := range chans {
		errs = maybeMergeErrs(errs, ch)
	}
	return
}

func maybeMergeErrs(car, cdr <-chan error) <-chan error {
	if car == nil {
		return cdr
	} else if cdr != nil {
		return mergeErrs(car, cdr)
	}
	return car
}

func combineErrs(errs <-chan error) error {
	combinedErrs := make([]error, 0, cap(errs))
	for e := range errs {
		combinedErrs = append(combinedErrs, e)
	}
	return flattenErr(combinedErrs...)
}

func mergeErrs(sources ...<-chan error) <-chan error {
	type (
		source   = <-chan error
		sourceRw = chan error
	)
	var (
		mergedWg  sync.WaitGroup
		mergedCh  = make(sourceRw)
		mergeFrom = func(ch source) {
			defer mergedWg.Done()
			for value := range ch {
				mergedCh <- value
			}
		}
	)
	mergedWg.Add(len(sources))
	for _, source := range sources {
		go mergeFrom(source)
	}
	go func() { mergedWg.Wait(); close(mergedCh) }()

	return mergedCh
}
