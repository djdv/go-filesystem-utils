package ipfs

import (
	"context"
	"io"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/ipfs/boxo/blockservice"
	coreiface "github.com/ipfs/boxo/coreiface"
	ipath "github.com/ipfs/boxo/coreiface/path"
	"github.com/ipfs/boxo/exchange"
	bsfetcher "github.com/ipfs/boxo/fetcher/impl/blockservice"
	"github.com/ipfs/boxo/path/resolver"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-unixfsnode"
	dagpb "github.com/ipld/go-codec-dagpb"
)

type (
	coreBlockStore struct {
		blocks   coreiface.BlockAPI
		validate bool
	}
	blockFetcher struct {
		coreiface.APIDagService
	}
)

func newPathResolver(api coreiface.CoreAPI) resolver.Resolver {
	var (
		blockstore = newCoreBlockStore(api.Block())
		fetcher    = makeBlockFetcher(api.Dag())
		service    = blockservice.New(blockstore, fetcher)
		config     = bsfetcher.NewFetcherConfig(service)
	)
	config.PrototypeChooser = dagpb.AddSupportToChooser(config.PrototypeChooser)
	fetcherFactory := config.WithReifier(unixfsnode.Reify)
	return resolver.NewBasicResolver(fetcherFactory)
}

func newCoreBlockStore(blocks coreiface.BlockAPI) *coreBlockStore {
	return &coreBlockStore{blocks: blocks}
}

func makeBlockFetcher(dag coreiface.APIDagService) exchange.Interface {
	return blockFetcher{APIDagService: dag}
}

func (bs *coreBlockStore) fetch(ctx context.Context, c cid.Cid) (blocks.Block, error) {
	blockReader, err := bs.blocks.Get(ctx, ipath.IpfsPath(c))
	if err != nil {
		return nil, err
	}
	blockData, err := io.ReadAll(blockReader)
	if err != nil {
		return nil, err
	}
	if bs.validate {
		nc, err := c.Prefix().Sum(blockData)
		if err != nil {
			return nil, blocks.ErrWrongHash
		}
		if !nc.Equals(c) {
			return nil, blocks.ErrWrongHash
		}
	}
	return blocks.NewBlockWithCid(blockData, c)
}

func (bs *coreBlockStore) Has(ctx context.Context, c cid.Cid) (bool, error) {
	blk, err := bs.fetch(ctx, c)
	if err != nil {
		return false, err
	}
	return blk != nil, nil
}

func (bs *coreBlockStore) Get(ctx context.Context, c cid.Cid) (blocks.Block, error) {
	blk, err := bs.fetch(ctx, c)
	if err != nil {
		return nil, err
	}
	return blk, nil
}

func (bs *coreBlockStore) GetSize(ctx context.Context, c cid.Cid) (int, error) {
	blk, err := bs.fetch(ctx, c)
	if err != nil {
		return 0, err
	}
	return len(blk.RawData()), nil
}

func (bs *coreBlockStore) HashOnRead(enabled bool) {
	bs.validate = enabled
}

func (*coreBlockStore) Put(context.Context, blocks.Block) error {
	return fserrors.ErrUnsupported
}

func (*coreBlockStore) PutMany(context.Context, []blocks.Block) error {
	return fserrors.ErrUnsupported
}

func (*coreBlockStore) AllKeysChan(ctx context.Context) (<-chan cid.Cid, error) {
	return nil, fserrors.ErrUnsupported
}

func (*coreBlockStore) DeleteBlock(context.Context, cid.Cid) error {
	return fserrors.ErrUnsupported
}

func (bf blockFetcher) GetBlock(ctx context.Context, c cid.Cid) (blocks.Block, error) {
	return bf.APIDagService.Get(ctx, c)
}

func (bf blockFetcher) GetBlocks(ctx context.Context, cids []cid.Cid) (<-chan blocks.Block, error) {
	out := make(chan blocks.Block)
	go func() {
		defer close(out)
		for _, c := range cids {
			block, err := bf.GetBlock(ctx, c)
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

func (blockFetcher) NotifyNewBlocks(ctx context.Context, blocks ...blocks.Block) error {
	return nil
}

func (blockFetcher) Close() error {
	return nil
}
