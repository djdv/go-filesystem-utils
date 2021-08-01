//go:build !nofuse
// +build !nofuse

package fscmds

import (
	"context"
	"fmt"

	config "github.com/ipfs/go-ipfs-config"
	configfile "github.com/ipfs/go-ipfs-config/serialize"
	"github.com/ipfs/go-ipfs/core/commands/filesystem/cgofuse"
	"github.com/ipfs/go-ipfs/filesystem"
	"github.com/ipfs/go-ipfs/filesystem/interface/ipfscore"
	"github.com/ipfs/go-ipfs/filesystem/interface/pinfs"
	"github.com/ipfs/go-ipfs/filesystem/manager"
	"github.com/ipfs/go-ipfs/filesystem/manager/errors"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

//TODO: provider caller options to select APIs
// TODO: extend core interface to support MFS and friends
func newCoreDispatchers(ctx context.Context, coreapi coreiface.CoreAPI) (dispatchMap, error) {
	var (
		fsb      manager.Binder
		err      error
		dispatch = make(dispatchMap)
		header   = requestHeader{API: filesystem.Fuse}
	)
	for _, nodeAPI := range []filesystem.ID{
		filesystem.IPFS,
		filesystem.IPNS,
		filesystem.PinFS,
	} {
		header.ID = nodeAPI
		switch nodeAPI {
		case filesystem.IPFS, filesystem.IPNS:
			fsb, err = cgofuse.NewBinder(ctx, ipfscore.NewInterface(ctx, coreapi, nodeAPI))
		case filesystem.PinFS:
			fsb, err = cgofuse.NewBinder(ctx, pinfs.NewInterface(ctx, coreapi))
		default:
			err = fmt.Errorf("unsupported API %v", nodeAPI) // TODO: better message
		}
		if err != nil {
			return nil, err
		}
		dispatch[header] = fsb
	}
	return dispatch, nil
}

func generatePipeline(ctx context.Context, requests manager.Requests) (sectionStream, errors.Stream) {
	withError := func(err error) (sectionStream, errors.Stream) {
		nodeErrors := make(chan error, 1)
		empty := make(chan section)
		nodeErrors <- err
		close(empty)
		close(nodeErrors)
		return empty, nodeErrors
	}

	confFile, err := config.Filename("") // TODO: argument from CLI
	if err != nil {
		return withError(err)
	}

	nodeConf, err := configfile.Load(confFile)
	switch err {
	case nil:
		return fillFromConfig(ctx, nodeConf, requests)
	case configfile.ErrNotInitialized:
		// TODO: we need to warn or something
		// no config file was found, nothing to check
		return splitRequests(ctx, requests)
	default:
		return withError(err)
	}
}
