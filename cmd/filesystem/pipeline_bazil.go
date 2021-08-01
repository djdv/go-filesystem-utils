//go:build !nofuse
// +build !nofuse

package fscmds

import (
	"context"

	"github.com/ipfs/go-ipfs/filesystem"
	"github.com/ipfs/go-ipfs/filesystem/manager"
)

// TODO:
// errors need to be added to the plex and logic itself
// e.g. we need to be able to consider half-primed states an error at some stage
// higher the better
// terminology needs to be checked (lisp magic)

// NOTE: we trust there to be no duplicate sections in the stream
// (run through a request filter before us)
func interlaceIPNSRequests(ctx context.Context, sections sectionStream) sectionStream {
	relay := make(chan section, len(sections))
	go func() {
		defer close(relay)

		// non-special cases get relayed as-is
		// special cases are stored and handled conditionally, last
		var promises struct{ ipfs, ipns *section }

		// TODO: name; `row` should be `section` but this conflicts with the type name
		for row := range sections {
			switch row.API {
			case filesystem.Fuse: // cache our special cases
				switch row.ID {
				case filesystem.IPFS:
					promises.ipfs = new(section)
					*promises.ipfs = row
					continue
				case filesystem.IPNS:
					promises.ipns = new(section)
					*promises.ipns = row
					continue
				}
			}
			select {
			case relay <- row:
			case <-ctx.Done():
				return
			}
		}
		// NOTE: legacy handling, IPNS interface depends on out-of-band IPFS coordination
		// (IPNS points to paths within an independent IPFS mountpoint)
		// we accommodate that by splicing the streams (if required)
		switch {
		case promises.ipns != nil:
			if promises.ipfs == nil { // TODO: panic -> error
				panic("IPNS requests depend on equal counts of IPFS requests")
			}
			var ipnsAux manager.Requests
			promises.ipfs.Requests, ipnsAux = cloneRequestStream(ctx, promises.ipfs.Requests)
			promises.ipns.Requests = spliceIpfsIpnsRequests(ctx, ipnsAux, promises.ipns.Requests)
			select {
			case relay <- *promises.ipns:
			case <-ctx.Done():
				return
			}
			fallthrough // The next case will use the cloned future values^ if we fallthrough
		case promises.ipfs != nil: // or the original promise values otherwise.
			select {
			case relay <- *promises.ipfs:
			case <-ctx.Done():
				return
			}
		}
	}()
	return relay
}

func cloneRequestStream(ctx context.Context, input manager.Requests) (_, _ manager.Requests) {
	out1, out2 := make(chan manager.Request), make(chan manager.Request)
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

// outputs sequential series: IPFS, IPNS, IPFS, IPNS, ...
func spliceIpfsIpnsRequests(ctx context.Context, ipfsSource manager.Requests, ipnsSource manager.Requests) manager.Requests {
	requests := make(chan manager.Request)
	go func() {
		defer close(requests)
		select {
		case requests <- <-ipfsSource:
		case <-ctx.Done():
			return
		}
		select {
		case requests <- <-ipnsSource:
		case <-ctx.Done():
			return
		}
	}()
	return requests
}
