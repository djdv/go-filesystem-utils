package pinfs

import (
	"context"
	"io/fs"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/filesystem/errors"
	ipfs "github.com/djdv/go-filesystem-utils/filesystem/ipfscore"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

const rootName = "."

type pinInterface struct {
	creationTime time.Time
	ctx          context.Context
	core         coreiface.CoreAPI
	ipfs         fs.FS
}

// TODO: WithIPFS(fs) option - re-use existing fs.FS instance from daemon
func NewInterface(ctx context.Context, core coreiface.CoreAPI) fs.FS {
	return &pinInterface{
		creationTime: time.Now(),
		ctx:          ctx,
		core:         core,
		ipfs:         ipfs.NewInterface(ctx, core, filesystem.IPFS),
	}
}

func (*pinInterface) ID() filesystem.ID { return filesystem.PinFS }

func (pi *pinInterface) Open(name string) (fs.File, error) {
	if name == rootName {
		return pi.OpenDir(name)
	}
	return pi.ipfs.Open(name)
}

func (pi *pinInterface) OpenDir(name string) (fs.ReadDirFile, error) {
	const op errors.Op = "pinfs.OpenDir"
	if name == rootName {
		ctx, cancel := context.WithCancel(pi.ctx)
		return &pinDirectory{
			ctx: ctx, cancel: cancel,
			stat:   (*rootStat)(&pi.creationTime),
			pinAPI: pi.core.Pin(),
			ipfs:   pi.ipfs,
		}, nil
	}

	ipfs, ok := pi.ipfs.(filesystem.OpenDirFS)
	if !ok {
		// TODO: better message
		return nil, errors.New(op,
			"OpenDir not supported by the provided IPFS fs.FS",
		)
	}
	return ipfs.OpenDir(name)
}

// TODO: close everything
func (*pinInterface) Close() error { return nil }

func (*pinInterface) Rename(_, _ string) error {
	const op errors.Op = "pinfs.Rename"
	// TODO: use abstract, consistent, error values
	// (^ this means reimplementing pkg `iferrors` with new Go conventions)
	// return errReadOnly
	return errors.New(op, errors.InvalidOperation)
}
