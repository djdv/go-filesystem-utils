package keyfs

import (
	"context"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-ipfs/filesystem"
	interfaceutils "github.com/ipfs/go-ipfs/filesystem/interface"
	iferrors "github.com/ipfs/go-ipfs/filesystem/interface/errors"
	"github.com/ipfs/go-ipfs/filesystem/interface/ipfscore"
	"github.com/ipfs/go-ipfs/filesystem/interface/ufs"
	ipld "github.com/ipfs/go-ipld-format"
	gomfs "github.com/ipfs/go-mfs"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	corepath "github.com/ipfs/interface-go-ipfs-core/path"
)

// TODO: docs
type keyInterface struct {
	ctx  context.Context
	core interfaceutils.CoreExtender

	ufs        ufs.UFS              // key `File` constructor
	references referenceTable       // the table which manages (shared) key `File` and `Interface` references
	ipns       filesystem.Interface // any requests to keys we don't own get proxied to ipns
}

// TODO: docs
func NewInterface(ctx context.Context, core coreiface.CoreAPI) filesystem.Interface {
	return &keyInterface{
		ctx:        ctx,
		core:       &interfaceutils.CoreExtended{CoreAPI: core},
		ufs:        ufs.NewInterface(ctx, core),
		ipns:       ipfscore.NewInterface(ctx, core, filesystem.IPNS),
		references: newReferenceTable(),
	}
}

func (ki *keyInterface) ID() filesystem.ID { return filesystem.KeyFS }
func (ki *keyInterface) Close() error      { return nil } // TODO: cascade close for all open references

// TODO: having both of these is dumb; do something about it
func (ki *keyInterface) publisherGenUFS(keyName string) ufs.ModifiedFunc {
	return func(nd ipld.Node) error {
		return localPublish(ki.ctx, ki.core, keyName, corepath.IpfsPath(nd.Cid()))
	}
}

// TODO: having both of these is dumb; do something about it
func (ki *keyInterface) publisherGenMFS(keyName string) gomfs.PubFunc {
	return func(ctx context.Context, cid cid.Cid) error {
		return localPublish(ctx, ki.core, keyName, corepath.IpfsPath(cid))
	}
}

// TODO: test this; quick port for now
// cross FS requests are going to be busted if we get them (/key -> /other-key/newhome)
func (ki *keyInterface) Rename(oldName, newName string) error {
	keyName, remainder := splitPath(oldName)
	if remainder == "" { // rename on key itself
		callCtx, cancel := interfaceutils.CallContext(ki.ctx)
		defer cancel()
		_, _, err := ki.core.Key().Rename(callCtx, keyName, newName[1:])
		if err != nil {
			return iferrors.IO(newName, err)
		}
		return nil
	}

	// subrequest
	fs, _, _, deferFunc, err := ki.selectFS(oldName)
	if err != nil {
		return err
	}
	defer deferFunc()

	_, subNewName := splitPath(newName)

	return fs.Rename(remainder, subNewName)
}
