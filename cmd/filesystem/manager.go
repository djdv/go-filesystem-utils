//go:build !nofuse
// +build !nofuse

package fscmds

import (
	"context"
	"fmt"
	"sync"

	"github.com/ipfs/go-ipfs/filesystem"
	"github.com/ipfs/go-ipfs/filesystem/manager"
	"github.com/ipfs/go-ipfs/filesystem/manager/errors"
	"github.com/multiformats/go-multiaddr"
)

// TODO: refactor if we can, complicated as-is
// TODO: async changed; review
func (ci *commandDispatcher) Bind(ctx context.Context, requests <-chan manager.Request) <-chan manager.Response {
	var (
		sections, errors = generatePipeline(ctx, requests)
		sectionResponses = make(chan manager.Responses, len(requests))

		wg               sync.WaitGroup
		respondWithError = func(err error) {
			defer wg.Done()
			errResp := make(chan manager.Response, 1)
			errResp <- manager.Response{Error: err}
			select {
			case sectionResponses <- errResp:
			case <-ctx.Done():
			}
			return
		}
		respondPrefixed = func(header requestHeader, responses manager.Responses) {
			defer wg.Done()
			select {
			case sectionResponses <- prefixResponses(ctx, header, responses):
			case <-ctx.Done():
				return
			}
		}
	)
	go func() {
		for sections != nil || errors != nil {
			select {
			case section, ok := <-sections:
				if !ok {
					sections = nil
					continue
				}
				header := section.requestHeader
				binder, ok := ci.dispatchers[header]
				if !ok {
					wg.Add(1)
					go respondWithError(fmt.Errorf("no binder found for: %v", header))
					continue
				}
				wg.Add(1)
				go respondPrefixed(header, binder.Bind(ctx, section.Requests))

			case err, ok := <-errors:
				if !ok {
					errors = nil
					continue
				}
				wg.Add(1)
				go respondWithError(err)
				continue

			case <-ctx.Done():
				return
			}
		}
		wg.Wait()
		close(sectionResponses)
	}()

	return handleResponses(ctx, ci.instanceIndex, mergeResponseStreams(ctx, sectionResponses))
}

// binder-requests will not contain our manager-header values,
// as such - binder-response values will not contain them either.
// We make sure to restore them before responding to the caller.
// (e.g. `/fuse/ipfs/path/mnt/ipfs` -> manager -> binder `/path/mnt/ipfs` ->
// manager `/fuse/ipfs/path/mnt/ipfs` -> ...)
func prefixResponses(ctx context.Context, header requestHeader, responses manager.Responses) manager.Responses {
	respChan := make(chan manager.Response)
	base, _ := multiaddr.NewComponent(header.API.String(), header.ID.String())
	go func() {
		defer close(respChan)
		for response := range responses {
			if response.Request != nil {
				response.Request = base.Encapsulate(response.Request)
			} else {
				response.Request = base
			}
			select {
			case respChan <- response:
			case <-ctx.Done():
				return
			}
		}
	}()
	return respChan
}

func mergeResponseStreams(ctx context.Context, responseStreams <-chan manager.Responses) manager.Responses {
	mergedStream := make(chan manager.Response)

	var wg sync.WaitGroup
	mergeFrom := func(responses manager.Responses) {
		defer wg.Done()
		for response := range responses {
			select {
			case mergedStream <- response:
			case <-ctx.Done():
				return
			}
		}
	}

	go func() {
		for responses := range responseStreams {
			wg.Add(1)
			go mergeFrom(responses)
		}
		wg.Wait()
		close(mergedStream)
	}()

	return mergedStream
}

type responseHandlerFunc func([]manager.Response) manager.Responses

// handleResponses processes and relays all input responses,
// either storing them in the `List` index or closing them (all) if an(y) error is encountered.
// NOTE: If the context is canceled, the returned stream is closed,
// but all input responses are still processed as described above.
func handleResponses(ctx context.Context, index instanceIndex, responses <-chan manager.Response) <-chan manager.Response {
	var (
		succeeded        []manager.Response
		relay                                = make(chan manager.Response)
		processResponses responseHandlerFunc = commitResponsesTo(index)
	)
	go func() {
		defer close(relay)
		for response := range responses {
			if ctx.Err() == nil { // caller is still listening
				select { // relay status
				case relay <- response:
				case <-ctx.Done():
				}
			}

			// regardless of callers attention,
			// handle the response
			if response.Error == nil {
				succeeded = append(succeeded, response)
			} else { // don't add this response
				processResponses = closeResponses // remap the finalizer to close responses
			}
		}

		// close or commit all valid responses we received
		for response := range processResponses(succeeded) {
			if ctx.Err() == nil { // relaying if the caller is still listening
				select {
				case relay <- response:
				case <-ctx.Done():
				}
			}
		}
	}()

	return relay
}

// commit these responses to the index, and return no additional status messages
func commitResponsesTo(index instanceIndex) responseHandlerFunc {
	return func(responses []manager.Response) manager.Responses {
		noResponse := make(chan manager.Response)
		close(noResponse)
		for i := range responses {
			instance := responses[i]
			key, _ := instance.Request.ValueForProtocol(int(filesystem.PathProtocol))
			index.store(key, &instance)
		}
		return noResponse
	}
}

// close these responses, and return typed error status messages
func closeResponses(responses []manager.Response) manager.Responses {
	undoneStatusMessages := make(chan manager.Response, len(responses))
	for i := len(responses) - 1; i != -1; i-- {
		instance := responses[i]
		if cErr := instance.Close(); cErr == nil {
			instance.Error = errors.Unwound
		} else {
			instance.Error = fmt.Errorf("%w - failed to close: %s", errors.Unwound, cErr)
		}
		undoneStatusMessages <- instance
	}
	close(undoneStatusMessages)
	return undoneStatusMessages
}

//TODO: move this somewhere else?
func cloneResponseStream(ctx context.Context, input manager.Responses) (_, _ manager.Responses) {
	out1, out2 := make(chan manager.Response), make(chan manager.Response)
	go func() {
		defer close(out1)
		defer close(out2)
		for request := range input {
			select {
			case out1 <- request:
			case <-ctx.Done():
				return
			}
			select {
			case out2 <- request:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out1, out2
}
