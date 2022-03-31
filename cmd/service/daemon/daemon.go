// Package daemon exposes the means to host a service instance
// and connect as a client.
package daemon

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/cmdsenv"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

func daemonPreRun(*cmds.Request, cmds.Environment) error {
	return filesystem.RegisterPathMultiaddr()
}

func daemonRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	settings, serviceEnv, err := parseCmds(ctx, request, env)
	if err != nil {
		return err
	}

	daemonCtx, daemonCancel := context.WithCancel(ctx)
	defer daemonCancel()
	muEmitter, emitErrs, err := synchronizeWithStdio(daemonCtx,
		emitter,
		os.Stdin, os.Stdout, os.Stderr,
	)
	if err != nil {
		return err
	}

	var (
		daemonEnv  = serviceEnv.Daemon()
		daemonPath = request.Path
		stopPath   = append(daemonPath, stop.Name)
	)
	stopperResponses, stopperReasons, err := setupStopperAPI(ctx, stopPath, daemonEnv)
	if err != nil {
		return err
	}
	// TODO: names
	goResponses, goErrs := setupGoStoppers(daemonCtx, request, daemonEnv.Stopper())

	listeners, listenErrs, err := settingsToListeners(ctx, request, settings)
	if err != nil {
		return err
	}
	var (
		serverListeners, serveErrs   = generateServers(ctx, request, serviceEnv, listeners)
		servers                      = startServers(ctx, serverListeners)
		serverResponses, serverCache = respondAndCache(ctx, servers)

		respond   = func(response *Response) error { return muEmitter.Emit(response) }
		responses = []responses{
			goResponses,
			stopperResponses,
			serverResponses,
		}
		errs = []errCh{
			goErrs,
			emitErrs,
			listenErrs,
			serveErrs,
		}

		exitCheckInterval = settings.AutoExitInterval
		exitWhenIdle      = exitCheckInterval != 0
	)
	if exitWhenIdle {
		idleResponses, idleErrs := stopOnIdleEvent(daemonCtx, serviceEnv, settings.AutoExitInterval)
		responses = append(responses, idleResponses)
		errs = append(errs, idleErrs)
	}

	var (
		emitResponses = func() error {
			if err := respond(startingResponse()); err != nil {
				return err
			}
			responses := CtxMerge(ctx, responses...)
			return ForEachOrError(ctx, responses, nil, respond)
		}

		stopReason cmdsenv.Reason
		wait       = func() error {
			var (
				finalErrs        = CtxMerge(ctx, errs...)
				cancelAndRespond = func(reason cmdsenv.Reason) error {
					daemonCancel()
					stopReason = reason
					response := stoppingResponse(reason)
					if err := respond(response); err != nil {
						return err
					}
					return nil
				}
			)
			return ForEachOrError(ctx, stopperReasons, finalErrs, cancelAndRespond)
		}
	)

	if err := emitResponses(); err != nil {
		return err
	}

	if err := wait(); err != nil {
		stopReason = cmdsenv.Error
		// TODO: wrap err?
		respond(stoppingResponse(stopReason))
		return err
	}

	shutdown := func(ctx context.Context, servers <-chan serverInstance) error {
		const shutdownGrace = 30 * time.Second
		var (
			shutdownMaddrs, shutdownErrs = shutdownServers(ctx, shutdownGrace, servers)
			broadcastShutdown            = func(maddr multiaddr.Multiaddr) error {
				return respond(maddrShutdownResponse(maddr, stopReason))
			}
		)
		return ForEachOrError(ctx, shutdownMaddrs, shutdownErrs, broadcastShutdown)
	}
	return handleStderr(shutdown(ctx, serverCache))
}

func setupGoStoppers(ctx context.Context, request *cmds.Request, stopper cmdsenv.Stopper) (responses, errCh) {
	var (
		signalResponses, signalErrs   = stopOnSignal(ctx, stopper, os.Interrupt)
		requestResponses, requestErrs = stopOnRequestCancel(ctx, stopper, request)
	)
	return CtxMerge(ctx, signalResponses, requestResponses),
		CtxMerge(ctx, signalErrs, requestErrs)
}

func respondAndCache(ctx context.Context,
	instances <-chan serverInstance,
) (responses, <-chan serverInstance) {
	const readyResponseCount = 1
	var (
		cacheWg sync.WaitGroup
		cache   = make([]serverInstance, 0, cap(instances)+readyResponseCount)
	)
	var (
		responses = make(chan *Response, cap(instances))
		respond   = func() {
			defer close(responses)
			defer cacheWg.Done()
			splitInstance := func(instance serverInstance) error {
				select {
				case responses <- maddrListenerResponse(instance.Multiaddr()):
					cache = append(cache, instance)
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			ForEachOrError(ctx, instances, nil, splitInstance)
			select {
			case responses <- readyResponse():
			case <-ctx.Done():
			}
		}
	)
	cacheWg.Add(1)
	go respond()

	relay := make(chan serverInstance, len(cache))
	go func() {
		cacheWg.Wait()
		defer close(relay)
		if ctx.Err() != nil {
			return
		}
		for _, instance := range cache {
			if ctx.Err() != nil {
				return
			}
			relay <- instance
		}
	}()
	return responses, relay
}

func waitForStopOrError(reasons <-chan cmdsenv.Reason, errs <-chan error) (responses, errCh) {
	var (
		responses = make(chan *Response, 1)
		outErrs   = make(chan error)
	)
	go func() {
		defer close(responses)
		defer close(outErrs)
		select {
		case reason := <-reasons:
			responses <- stoppingResponse(reason)
		case err := <-errs:
			responses <- stoppingResponse(cmdsenv.Error)
			outErrs <- err
			for err := range errs {
				outErrs <- err
			}
		}
	}()
	return responses, outErrs
}
