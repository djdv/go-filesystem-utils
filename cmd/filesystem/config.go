package fscmds

import (
	"context"
	"fmt"

	config "github.com/ipfs/go-ipfs-config"
	"github.com/ipfs/go-ipfs/filesystem"
	"github.com/ipfs/go-ipfs/filesystem/manager"
	"github.com/ipfs/go-ipfs/filesystem/manager/errors"
	"github.com/multiformats/go-multiaddr"
)

// fillFromConfig checks requests against sections in the config.
// If the request is missing values and the config has a section for it,
// the value is substituted with the config's in the output streams.
// Unrecognized requests with valid headers are relayed as-is in their section stream.
func fillFromConfig(ctx context.Context,
	config *config.Config, requests manager.Requests) (sectionStream, errors.Stream) {
	relay, combinedErrors := make(chan section), make(chan errors.Stream, 1)
	sections, sectionErrors := splitRequests(ctx, requests)
	combinedErrors <- sectionErrors

	go func() {
		defer close(relay)
		defer close(combinedErrors)
		for section := range sections {
			switch section.API {
			case filesystem.Fuse:
				fuseRequests, fuseErrors := fillFuseConfig(ctx, config, section.ID, section.Requests)
				section.Requests = fuseRequests
				select {
				case combinedErrors <- fuseErrors:
				case <-ctx.Done():
					return
				}
			}

			select { // send the (potentially re-routed) section
			case relay <- section:
			case <-ctx.Done():
				return
			}
		}
	}()

	return relay, errors.Splice(ctx, combinedErrors)
}

// provides values for requests, from config
func fillFuseConfig(ctx context.Context, nodeConf *config.Config,
	nodeAPI filesystem.ID, requests manager.Requests) (manager.Requests, errors.Stream) {
	relay, errors := make(chan manager.Request), make(chan error)

	go func() {
		defer close(relay)
		defer close(errors)
		var err error
		for request := range requests {
			if request == nil { // request contains no (body) value (header only), use default value below
				err = multiaddr.ErrProtocolNotFound
			} else { //  request may contain the value we expect, check for it and handle error below
				_, err = request.ValueForProtocol(int(filesystem.PathProtocol))
			}
			switch err {
			case nil: // request has expected values, proceed
			case multiaddr.ErrProtocolNotFound: // request is missing a target value
				switch nodeAPI { // supply one from the config's value
				case filesystem.IPFS:
					request, err = multiaddr.NewComponent(filesystem.PathProtocol.String(),
						nodeConf.Mounts.IPFS)
				case filesystem.IPNS:
					request, err = multiaddr.NewComponent(filesystem.PathProtocol.String(),
						nodeConf.Mounts.IPNS)
				default:
					err = fmt.Errorf("protocol %v has no config value", nodeAPI)
				}
			}
			if err != nil {
				select {
				case errors <- err:
				case <-ctx.Done():
				}
				return
			}
			select {
			case relay <- request:
			case <-ctx.Done():
				return
			}
		}
	}()

	return relay, errors
}
