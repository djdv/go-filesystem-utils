package ipfs

import (
	"context"
	"io"
	"io/fs"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem/errors"
	gofs "github.com/djdv/go-filesystem-utils/filesystem/go"
	ipld "github.com/ipfs/go-ipld-format"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	corepath "github.com/ipfs/interface-go-ipfs-core/path"
)

type rootDirectory time.Time // Time of our creation.

func (r *rootDirectory) Stat() (fs.FileInfo, error) { return (*rootStat)(r), nil }

func (r *rootDirectory) Read([]byte) (int, error) {
	const op errors.Op = "root.Read"
	return -1, errors.New(op, errors.IsDir)
}

func (r *rootDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	if count > 0 {
		return nil, io.EOF
	}
	return nil, nil
}

func (r *rootDirectory) Close() error { return nil }

// TODO: we should probablt split sequential directories from stream variants
// one can embed the other - stream should be lighter and preferred (like the original was)
type coreDirectory struct {
	ctx    context.Context
	cancel context.CancelFunc
	core   coreiface.CoreAPI
	path   corepath.Path
	stat   statFunc
	crtime time.Time

	// Used in Stream
	entries <-chan coreiface.DirEntry

	// Used in Read
	transformed <-chan fs.DirEntry
	errs        <-chan error
}

func openIPFSDir(ctx context.Context,
	core coreiface.CoreAPI, ipldNode ipld.Node,
	statFn statFunc, crtime time.Time) (fs.ReadDirFile, error) {
	ctx, cancel := context.WithCancel(ctx)
	return &coreDirectory{
		ctx: ctx, cancel: cancel,
		core:   core,
		path:   corepath.IpfsPath(ipldNode.Cid()),
		stat:   statFn,
		crtime: crtime,
	}, nil
}

func (cd *coreDirectory) Stat() (fs.FileInfo, error) { return cd.stat() }

func (*coreDirectory) Read([]byte) (int, error) {
	const op errors.Op = "coreDirectory.Read"
	return -1, errors.New(op, errors.IsDir)
}

func (cd *coreDirectory) StreamDir(ctx context.Context, output chan<- fs.DirEntry) <-chan error {
	const op errors.Op = "coreDirectory.StreamDir"
	entries := cd.entries
	if entries == nil {
		unixChan, err := cd.core.Unixfs().Ls(cd.ctx, cd.path)
		if err != nil {
			errs := make(chan error, 1)
			errs <- errors.New(op,
				errors.IO,
				err,
			)
			close(errs)
			return errs
		}
		entries = unixChan
		cd.entries = entries
	}

	// TODO: This time value is going to depend on the FS type.
	// For IPFS we should use the mount time.
	// For formats that have it (ufs1.5>=) we should pull it from the data.
	var (
		crtime = cd.crtime
		errs   = make(chan error)
	)
	go func() {
		defer close(output)
		for entries != nil {
			select {
			case ent, ok := <-entries:
				if !ok {
					entries = nil
					cd.entries = nil // FIXME: thread safety
					break
				}
				if err := ent.Err; err != nil {
					select {
					case errs <- err:
						continue
					case <-ctx.Done():
						return
					}
				}
				select {
				case output <- &ufsDirEntry{DirEntry: ent, crtime: crtime}:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return errs
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func (cd *coreDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	var (
		entries = cd.transformed
		errs    = cd.errs
	)
	if entries == nil {
		output := make(chan fs.DirEntry, max(0, count))
		errs = cd.StreamDir(cd.ctx, output)
		cd.transformed = output
		cd.errs = errs
		entries = output
	}
	return gofs.ReadDir(count, entries)
}

func (cd *coreDirectory) Close() error {
	const op errors.Op = "coredir.Close"
	cancel := cd.cancel
	cd.cancel = nil
	if cancel == nil {
		return errors.New(op,
			errors.InvalidItem, // TODO: Check POSIX expected values
			"directory was not open",
		)
	}
	cancel()
	return nil
}

type ufsDirEntry struct {
	crtime time.Time
	coreiface.DirEntry
}

func (de *ufsDirEntry) Name() string { return de.DirEntry.Name }

func (de *ufsDirEntry) Info() (fs.FileInfo, error) {
	return &stat{
		name:   de.DirEntry.Name,
		typ:    de.DirEntry.Type,
		size:   de.DirEntry.Size,
		crtime: de.crtime,
	}, nil
}

func (de *ufsDirEntry) Type() fs.FileMode {
	info, err := de.Info()
	if err != nil {
		return fs.ModeIrregular
	}
	return info.Mode() & fs.ModeType
}

func (de *ufsDirEntry) IsDir() bool { return de.Type()&fs.ModeDir != 0 }
