package mfs

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem/errors"
	gofs "github.com/djdv/go-filesystem-utils/filesystem/go"
	"github.com/ipfs/go-mfs"
)

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
	mfsDir *mfs.Directory
	nodes  []mfs.NodeListing
}

func (md *mfsDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	const op errors.Op = "mfsDirectory.ReadDir"

	nodes := md.nodes
	if nodes == nil {
		listings, err := md.mfsDir.List(md.ctx)
		if err != nil {
			return nil, errors.New(op, err) // TODO we could probably add more context
		}
		nodes = listings
		md.nodes = nodes
	}

	entries := make(chan fs.DirEntry, len(nodes))
	go func() {
		for _, node := range nodes {
			select {
			case entries <- &mfsDirEntry{
				node:         node,
				creationTime: *(*time.Time)(md.stat),
			}:
			case <-md.ctx.Done():
				return
			}
		}
	}()
	return gofs.ReadDir(count, entries)
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
