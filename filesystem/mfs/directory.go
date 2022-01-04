package mfs

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem/errors"
	"github.com/ipfs/go-mfs"
)

type entsByName []fs.DirEntry

func (ents entsByName) Len() int           { return len(ents) }
func (ents entsByName) Swap(i, j int)      { ents[i], ents[j] = ents[j], ents[i] }
func (ents entsByName) Less(i, j int) bool { return ents[i].Name() < ents[j].Name() }

func (md *mfsDirectory) Stat() (fs.FileInfo, error) { return md.stat, nil }

func (*mfsDirectory) Read([]byte) (int, error) {
	const op errors.Op = "mfsDirectory.Read"
	return -1, errors.New(op, errors.IsDir)
}

func (mi *mfsInterface) OpenDir(name string) (fs.ReadDirFile, error) {
	const op errors.Op = "mfs.OpenDir"
	var (
		mfsNode mfs.FSNode
		err     error
	)
	if name == rootName {
		mfsNode, err = mfs.Lookup(mi.mroot, "/")
	} else {
		mfsNode, err = mfs.Lookup(mi.mroot, path.Join("/", name))
	}
	if err != nil {
		return nil, errors.New(op,
			errors.Path(name),
			err,
		)
	}

	mfsDir, isDir := mfsNode.(*mfs.Directory)
	if !isDir {
		return nil, errors.New(op,
			errors.Path(name),
			fmt.Errorf("type %v != %v (directory)",
				mfsNode.Type(),
				mfs.TDir),
			errors.NotDir,
		)
	}

	ctx, cancel := context.WithCancel(mi.ctx)
	return &mfsDirectory{
		ctx: ctx, cancel: cancel,
		stat:   (*rootStat)(&mi.creationTime),
		mfsDir: mfsDir,
	}, nil
}

type mfsDirectory struct {
	ctx    context.Context
	cancel context.CancelFunc
	stat   *rootStat // TODO: don't store this here; generate a rootstat on demand
	// store creation time here instead of the magic type cast+copy we do with the rootStat
	mfsDir  *mfs.Directory
	entries []mfs.NodeListing
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func (md *mfsDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	const op errors.Op = "mfsDirectory.ReadDir"

	entries := md.entries
	if entries == nil {
		ents, err := md.mfsDir.List(md.ctx)
		if err != nil {
			return nil, errors.New(op, err) // TODO we could probably add more context
		}
		entries = ents
		md.entries = entries
	}

	var ents []fs.DirEntry
	if count <= 0 {
		// NOTE: [spec] This will cause the loop below to become infinite.
		// This is intended by the fs.FS spec
		count = -1
		ents = make([]fs.DirEntry, 0, len(entries))
	} else {
		// If we're dealing with a finite amount, allocate for it.
		// NOTE: If the caller passes an unreasonably large `count`,
		// we do nothing to protect against OOM.
		// This is to be considered a client-side implementation error
		// and should be fixed caller side.
		ents = make([]fs.DirEntry, 0, count)
	}

	for _, ent := range entries {
		if count == 0 {
			break
		}
		ents = append(ents, &mfsDirEntry{node: ent, creationTime: *(*time.Time)(md.stat)})
		count--
	}
	md.entries = entries[len(ents):]

	sort.Sort(entsByName(ents))

	return ents, nil
}

func (md *mfsDirectory) Close() error {
	const op errors.Op = "mfsDirectory.Close"
	cancel := md.cancel
	md.cancel = nil
	if cancel == nil {
		return errors.New(op,
			errors.InvalidItem, // TODO: Check POSIX expected values
			"directory was not open",
		)
	}
	cancel()
	return nil
}

type mfsDirEntry struct {
	node         mfs.NodeListing
	creationTime time.Time
}

func (me *mfsDirEntry) Name() string { return me.node.Name }

func (me *mfsDirEntry) Info() (fs.FileInfo, error) {
	return &entryStat{node: me.node, creationTime: me.creationTime}, nil
}

func (me *mfsDirEntry) Type() fs.FileMode {
	info, err := me.Info()
	if err != nil {
		return fs.ModeIrregular
	}
	return info.Mode() & fs.ModeType
}

func (me *mfsDirEntry) IsDir() bool { return me.Type()&fs.ModeDir != 0 }

type entryStat struct {
	node         mfs.NodeListing
	creationTime time.Time
}

func (es *entryStat) Name() string       { return es.node.Name }
func (es *entryStat) Size() int64        { return es.node.Size }
func (es *entryStat) ModTime() time.Time { return es.creationTime }
func (es *entryStat) IsDir() bool        { return es.Mode().IsDir() } // [spec] Don't hardcode this.
func (es *entryStat) Sys() interface{}   { return es }
func (es *entryStat) Mode() fs.FileMode {
	// TODO: we should just stat the full node up front
	// mfs ents don't give us enough information about the type here
	switch mfs.NodeType(es.node.Type) {
	case mfs.TFile:
		return fs.FileMode(0)
	case mfs.TDir:
		return fs.ModeDir
	default:
		return fs.ModeIrregular
	}
}
