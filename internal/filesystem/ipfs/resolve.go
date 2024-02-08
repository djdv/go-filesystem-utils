package ipfs

import (
	"context"
	"errors"

	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/exchange"
	bsfetcher "github.com/ipfs/boxo/fetcher/impl/blockservice"
	"github.com/ipfs/boxo/path/resolver"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/ipfs/go-unixfsnode"
	dagpb "github.com/ipld/go-codec-dagpb"
)

type (
	fnBlockStore   getNodeFunc
	fnBlockFetcher getNodeFunc
	getNodeFunc    func(cid cid.Cid) (ipld.Node, error)
)

func newPathResolver(getNodeFn getNodeFunc) resolver.Resolver {
	var (
		blockstore = newCoreBlockStore(getNodeFn)
		fetcher    = makeBlockFetcher(getNodeFn)
		service    = blockservice.New(blockstore, fetcher)
		config     = bsfetcher.NewFetcherConfig(service)
	)
	config.PrototypeChooser = dagpb.AddSupportToChooser(config.PrototypeChooser)
	fetcherFactory := config.WithReifier(unixfsnode.Reify)
	return resolver.NewBasicResolver(fetcherFactory)
}

func newCoreBlockStore(getNodeFn getNodeFunc) fnBlockStore {
	return fnBlockStore(getNodeFn)
}

func makeBlockFetcher(getNodeFn getNodeFunc) exchange.Interface {
	return fnBlockFetcher(getNodeFn)
}

func (getNodeFn fnBlockStore) Has(_ context.Context, c cid.Cid) (bool, error) {
	blk, err := getNodeFn(c)
	if err != nil {
		return false, err
	}
	return blk != nil, nil
}

func (getNodeFn fnBlockStore) Get(_ context.Context, c cid.Cid) (blocks.Block, error) {
	return getNodeFn(c)
}

func (getNodeFn fnBlockStore) GetSize(ctx context.Context, c cid.Cid) (int, error) {
	blk, err := getNodeFn(c)
	if err != nil {
		return 0, err
	}
	return len(blk.RawData()), nil
}

func (fnBlockStore) HashOnRead(bool) {}

func (fnBlockStore) Put(context.Context, blocks.Block) error {
	return errors.ErrUnsupported
}

func (fnBlockStore) PutMany(context.Context, []blocks.Block) error {
	return errors.ErrUnsupported
}

func (fnBlockStore) AllKeysChan(ctx context.Context) (<-chan cid.Cid, error) {
	return nil, errors.ErrUnsupported
}

func (fnBlockStore) DeleteBlock(context.Context, cid.Cid) error {
	return errors.ErrUnsupported
}

func (getNodeFn fnBlockFetcher) GetBlock(_ context.Context, c cid.Cid) (blocks.Block, error) {
	return getNodeFn(c)
}

func (blockGetter fnBlockFetcher) GetBlocks(ctx context.Context, cids []cid.Cid) (<-chan blocks.Block, error) {
	out := make(chan blocks.Block)
	go func() {
		defer close(out)
		for _, c := range cids {
			block, err := blockGetter.GetBlock(ctx, c)
			if err != nil {
				return
			}
			select {
			case out <- block:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (fnBlockFetcher) NotifyNewBlocks(ctx context.Context, blocks ...blocks.Block) error {
	return nil
}

func (fnBlockFetcher) Close() error {
	return nil
}
