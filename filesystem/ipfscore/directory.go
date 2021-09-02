package ipfs

import (
	"context"
	"io"
	"io/fs"
	"sort"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem/errors"
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

func (r *rootDirectory) ReadDir(n int) ([]fs.DirEntry, error) { return nil, io.EOF }

func (r *rootDirectory) Close() error { return nil }

type coreDirectory struct {
	ctx     context.Context
	cancel  context.CancelFunc
	core    coreiface.CoreAPI
	path    corepath.Path
	entries <-chan coreiface.DirEntry
	stat    statFunc
}

// TODO: we should either implement this on the underlying type
// (accumulate raws, sort raws, convert raws to dirents) (<-do this one)
// or de-dupe this generic one across interfaces that need it
type ufsByName []fs.DirEntry

func (dirents ufsByName) Len() int           { return len(dirents) }
func (dirents ufsByName) Swap(i, j int)      { dirents[i], dirents[j] = dirents[j], dirents[i] }
func (dirents ufsByName) Less(i, j int) bool { return dirents[i].Name() < dirents[j].Name() }

func openIPFSDir(ctx context.Context,
	core coreiface.CoreAPI, ipldNode ipld.Node, statFn statFunc) (fs.ReadDirFile, error) {
	ctx, cancel := context.WithCancel(ctx)
	return &coreDirectory{
		ctx: ctx, cancel: cancel,
		core: core,
		path: corepath.IpfsPath(ipldNode.Cid()),
		stat: statFn,
	}, nil
}

func (cd *coreDirectory) Stat() (fs.FileInfo, error) { return cd.stat() }

func (*coreDirectory) Read([]byte) (int, error) {
	const op errors.Op = "coreDirectory.Read"
	return -1, errors.New(op, errors.IsDir)
}

func (cd *coreDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	const op errors.Op = "coreDirectory.ReadDir"
	entries := cd.entries
	if entries == nil {
		unixChan, err := cd.core.Unixfs().Ls(cd.ctx, cd.path)
		if err != nil {
			return nil, errors.New(op,
				errors.IO,
				err,
			)
		}
		entries = unixChan
		cd.entries = entries
	}

	var (
		ents []fs.DirEntry
		err  error
	)
	if count <= 0 {
		// NOTE: [spec] This will cause the loop below to become infinite.
		// This is intended by the fs.FS spec
		count = -1
	} else {
		// If we're dealing with a finite amount, allocate for it.
		// NOTE: If the caller passes an unreasonably large `count`,
		// we do nothing to protect against OOM.
		// This is to be considered a client-side implementation error
		// and should be fixed caller side.
		ents = make([]fs.DirEntry, 0, count)
	}

	// TODO: this time value is going to depend
	// for IPFS we should use the mount time
	// for everything else it should be generated now?
	// For formats that have it (ufs1.5>=) we should pull it from the data.
	crtime := time.Now()
	for ; count != 0; count-- {
		ent, ok := <-entries
		if !ok {
			err = io.EOF
			break
		}
		ents = append(ents, &ufsDirEntry{DirEntry: ent, crtime: crtime})
	}

	sort.Sort(ufsByName(ents))

	return ents, err
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
	coreiface.DirEntry
	crtime time.Time
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
