// Package daemon exposes the means to host a service instance
// and connect as a client.
package daemon

import (
	"context"
	"os"
	"sync"

	"github.com/djdv/go-filesystem-utils/internal/cmds/environment/stop"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

type errCh = <-chan error

func todo_name_DaemonRun(ctx context.Context, settings Settings) {
	// TODO: break out the daemon.Stopper interface into some higher pkg
	// that the daemon functions can use (irrelevant of cmdsenv).
	// Pass it here and use it along with the rest of run's non-cmds body.
	//
	//Re-use settings struct (just pass it after parsing from cmds)
}

func setupGoStoppers(ctx context.Context, request *cmds.Request, stopper stop.Stopper) (responses, errCh) {
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

func waitForStopOrError(reasons <-chan stop.Reason, errs <-chan error) (responses, errCh) {
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
			responses <- stoppingResponse(stop.Error)
			outErrs <- err
			for err := range errs {
				outErrs <- err
			}
		}
	}()
	return responses, outErrs
}
