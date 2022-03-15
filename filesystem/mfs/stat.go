package mfs

import (
	"io/fs"
	"path"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem/errors"
	"github.com/ipfs/go-mfs"
	"github.com/ipfs/go-unixfs"
)

type rootStat time.Time

func (rs *rootStat) Name() string       { return rootName }
func (rs *rootStat) Size() int64        { return 0 }
func (rs *rootStat) Mode() fs.FileMode  { return fs.ModeDir }
func (rs *rootStat) ModTime() time.Time { return *(*time.Time)(rs) }
func (rs *rootStat) IsDir() bool        { return rs.Mode().IsDir() } // [spec] Don't hardcode this.
func (rs *rootStat) Sys() interface{}   { return rs }

func (mi *mfsInterface) Stat(name string) (fs.FileInfo, error) {
	const op errors.Op = "mfs.Stat"
	if name == rootName {
		return (*rootStat)(&mi.creationTime), nil
	}

	// TODO: is there a direct way to do this?
	mfsNode, err := mfs.Lookup(mi.mroot, path.Join("/", name))
	if err != nil {
		return nil, errors.New(op, err) // TODO: context
	}
	ipldNode, err := mfsNode.GetNode()
	if err != nil {
		return nil, errors.New(op, err) // TODO: context
	}
	ufsNode, err := unixfs.ExtractFSNode(ipldNode)
	if err != nil {
		return nil, errors.New(op, err) // TODO: context
	}

	var typ fs.FileMode
	switch mfsNode.Type() {
	case mfs.TFile:
		typ = fs.FileMode(0)
	case mfs.TDir:
		typ = fs.ModeDir
	default:
		// Symlinks are not natively supported by MFS / the Files API
		// (But we'll support them)
		nodeType := ufsNode.Type()
		if nodeType == unixfs.TSymlink {
			typ = fs.ModeSymlink
			break
		}
		typ = fs.ModeIrregular
	}
	return &ipldStat{
		name:         path.Base(name),
		size:         int64(ufsNode.FileSize()),
		typ:          typ,
		creationTime: mi.creationTime,
	}, nil
}

type ipldStat struct {
	name         string
	size         int64
	typ          fs.FileMode
	creationTime time.Time
}

func (is *ipldStat) Name() string       { return is.name }
func (is *ipldStat) Size() int64        { return is.size }
func (is *ipldStat) Mode() fs.FileMode  { return is.typ }
func (is *ipldStat) ModTime() time.Time { return is.creationTime }
func (is *ipldStat) IsDir() bool        { return is.Mode().IsDir() } // [spec] Don't hardcode this.
func (is *ipldStat) Sys() interface{}   { return is }
