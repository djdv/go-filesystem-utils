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

type (
	requestHeader struct {
		filesystem.API
		filesystem.ID
	}

	section struct {
		requestHeader
		manager.Requests
	}
	sectionStream = <-chan section
)

// splitRequest returns each component of the request, as an individual typed values.
func splitRequest(request manager.Request) (hostAPI filesystem.API, nodeAPI filesystem.ID, remainder manager.Request, err error) {
	// NOTE: we expect the request to contain a pair of API values as its first component (e.g. `/fuse/ipfs/`)
	// with or without a remainder (e.g. remainder may be `nil`, `.../path/mnt/ipfs/...`, etc.)
	defer func() { // multiaddr pkg will panic if the request is malformed
		if grace := recover(); grace != nil { // so we exorcise the goroutine if this happens
			err = fmt.Errorf("splitRequest panicked: %v - %v", request, grace)
		}
	}()

	var ( // decapsulation
		header                     *multiaddr.Component
		hostProtocol, nodeProtocol int
	)
	header, remainder = multiaddr.SplitFirst(request)
	hostProtocol = header.Protocol().Code
	if nodeProtocol, _, err = multiaddr.ReadVarintCode(header.RawValue()); err != nil {
		return
	}

	// disambiguation
	// Note the direct use of the return variables in the range clauses.
	// If both values being inspected appear in our supported list, we'll return them.
	supportedAPIs := []filesystem.ID{
		filesystem.IPFS,
		filesystem.IPNS,
		filesystem.PinFS,
		//filesystem.KeyFS,
		//filesystem.Files,
	}
	for _, hostAPI = range []filesystem.API{
		filesystem.Fuse,
	} {
		if hostAPI == filesystem.API(hostProtocol) {
			for _, nodeAPI = range supportedAPIs {
				if nodeAPI == filesystem.ID(nodeProtocol) {
					return
				}
			}
		}
	}

	err = fmt.Errorf("unsupported API pair: %v in request %v", header, request)
	return
}

// splitRequests divides the request stream into a series of sections,
// deliniated by request header data.
func splitRequests(ctx context.Context, requests manager.Requests) (sectionStream, errors.Stream) {
	sections, errors := make(chan section), make(chan error)
	sectionIndex := make(map[requestHeader]chan manager.Request) // TODO: aloc:cIDStart-cEnd

	go func() {
		defer close(sections)
		defer close(errors)
		var requestsWg sync.WaitGroup
		for request := range requests {
			hostAPI, nodeAPI, body, err := splitRequest(request)
			if err != nil {
				select {
				case errors <- err:
				case <-ctx.Done():
				}
				return
			}

			header := requestHeader{API: hostAPI, ID: nodeAPI}
			requestDestination, alreadyMade := sectionIndex[header]

			if !alreadyMade {
				requestDestination = make(chan manager.Request)
				sectionIndex[header] = requestDestination
				select { // send the (new) section to the caller
				case sections <- section{
					requestHeader: header,
					Requests:      requestDestination,
				}:
				case <-ctx.Done():
					return
				}
			}

			// send this request body to its specific section
			requestsWg.Add(1)
			go func() {
				defer requestsWg.Done()
				select {
				case requestDestination <- body:
				case <-ctx.Done():
					return
				}
			}()
		}
		go func() { // wait for all requests to be sent before closing substreams
			requestsWg.Wait()
			for _, sectionStream := range sectionIndex {
				close(sectionStream)
			}
		}()
	}()

	return sections, errors
}
