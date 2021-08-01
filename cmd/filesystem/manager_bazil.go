//go:build !nofuse
// +build !nofuse

package fscmds

import (
	"context"

	"github.com/ipfs/go-ipfs/core"
	bazil "github.com/ipfs/go-ipfs/core/commands/filesystem/bazil"
	"github.com/ipfs/go-ipfs/filesystem"
	"github.com/ipfs/go-ipfs/filesystem/manager"
	"github.com/ipfs/go-ipfs/filesystem/manager/errors"
)

func NewNodeInterface(ctx context.Context, node *core.IpfsNode) (manager.Interface, error) {
	cd := &commandDispatcher{
		index:    newIndex(),
		dispatch: make(dispatchMap),
		IpfsNode: node,
	}

	//TODO: provider caller options to select fuse API
	for _, api := range []filesystem.ID{
		filesystem.IPFS,
		filesystem.IPNS,
	} {
		fsb, err := bazil.NewBinder(ctx, api, node, false) // TODO: pull option from config
		if err != nil {
			return nil, err
		}
		cd.dispatch[requestHeader{API: filesystem.Fuse, ID: api}] = fsb
	}

	return cd, nil
}

func generatePipeline(ctx context.Context, node *core.IpfsNode, requests manager.Requests) (sectionStream, errors.Stream) {
	nodeConf, err := node.Repo.Config() // TODO: test that this picks up config changes between requests
	if err != nil {
		nodeErrors := make(chan error, 1)
		empty := make(chan section)
		nodeErrors <- err
		close(empty)
		close(nodeErrors)
		return empty, nodeErrors
	}
	sections, configErrors := fillFromConfig(ctx, nodeConf, requests)
	return interlaceIPNSRequests(ctx, sections), configErrors
}
