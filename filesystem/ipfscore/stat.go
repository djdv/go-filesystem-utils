package ipfs

import (
	"fmt"
	"io/fs"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/filesystem/errors"
	ipld "github.com/ipfs/go-ipld-format" // TODO: migrate to new standard
	dag "github.com/ipfs/go-merkledag"
	"github.com/ipfs/go-unixfs"
	unixpb "github.com/ipfs/go-unixfs/pb"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

type statFunc func() (fs.FileInfo, error)

type rootStat = rootDirectory

func (rs *rootStat) Name() string       { return rootName }
func (rs *rootStat) Size() int64        { return 0 }
func (rs *rootStat) Mode() fs.FileMode  { return fs.ModeDir }
func (rs *rootStat) ModTime() time.Time { return *(*time.Time)(rs) }
func (rs *rootStat) IsDir() bool        { return rs.Mode().IsDir() } // [spec] Don't hardcode this.
func (rs *rootStat) Sys() interface{}   { return rs }

type stat struct {
	name      string
	typ       coreiface.FileType // Reserved keyword - following `reflect`'s convention
	size      uint64
	blockSize uint64
	blocks    uint64
	/* UFS1.5 and UFS2 implementations do not yet exist in Go.
	ATimeNano int64
	MTimeNano int64
	CTimeNano int64
	*/
	// No current variant of UFS supports creation time.
	// For UFS1 this value is expected to be superficially imposed.
	// E.g. a value representing the time of
	// process creation, mount point binding, `fs.File` instantiation, etc.
	// Whatever seems most appropriate for the implementation and usage thereof.
	crtime time.Time
}

func (st *stat) Name() string { return st.name }
func (st *stat) Size() int64  { return int64(st.size) }
func (st *stat) Mode() fs.FileMode {
	switch st.typ {
	case coreiface.TDirectory:
		return fs.ModeDir
	case coreiface.TFile:
		return fs.FileMode(0)
	case coreiface.TSymlink:
		return fs.ModeSymlink
	default:
		panic(fmt.Errorf(
			"Mode: stat contains unexpected type: %v",
			st.typ,
		))
	}
}

func (st *stat) ModTime() time.Time { return st.crtime }
func (st *stat) IsDir() bool        { return st.Mode().IsDir() }
func (st *stat) Sys() interface{}   { return st }

func (ci *coreInterface) Stat(name string) (fs.FileInfo, error) {
	const op errors.Op = "ipfscore.Stat"

	if name == rootName {
		return (*rootStat)(&ci.creationTime), nil
	}

	ipfsPath := goToIPFSCore(ci.systemID, name)
	ipldNode, err := resolveNode(ci.ctx, ci.core, ipfsPath)
	if err != nil {
		return nil, errors.New(op,
			errors.Path(name),
			errors.IO, // TODO: [review] double check this Kind makes sense for this.
			err,
		)
	}

	stat, err := statNode(ipldNode)
	if err != nil {
		return nil, errors.New(op,
			errors.Path(name),
			errors.Other,
			err,
		)
	}
	return stat, nil
}

func statNode(ipldNode ipld.Node) (stat *stat, err error) {
	if typedNode, ok := ipldNode.(*dag.ProtoNode); ok {
		ufsNode, err := unixfs.ExtractFSNode(typedNode)
		if err != nil {
			return nil, err
		}
		stat, err = unixFSAttr(ufsNode)
	} else { //  *dag.RawNode, *cbor.Node
		stat, err = genericAttr(ipldNode)
	}
	return
}

func genericAttr(genericNode ipld.Node) (*stat, error) {
	attr := new(stat)
	// raw nodes only contain data so we'll treat them as a flat file
	// cbor nodes are not currently supported via UnixFS so we assume them to contain only data
	// TODO: review ^ is there some way we can implement this that won't blow up in the future?
	// (if unixfs supports cbor and directories are implemented to use them )
	attr.typ = coreiface.TFile

	nodeStat, err := genericNode.Stat()
	if err != nil {
		return attr, err
	}

	attr.size = uint64(nodeStat.CumulativeSize)
	//attr.BlockSize= uint64(nodeStat.BlockSize)

	return attr, nil
}

func unixFSAttr(ufsNode *unixfs.FSNode) (*stat, error) {
	var attr stat
	attr.typ = unixfsTypeToCoreType(ufsNode.Type())

	/* TODO: [port]
	// NOTE: we can't account for variable block size so we use the size of the first block only (if any)
	blocks := len(ufsNode.BlockSizes())
	if blocks > 0 {
		attr.BlockSize = ufsNode.BlockSize(0)
		attr.Blocks = uint64(blocks)
	}
	*/

	attr.size = ufsNode.FileSize()

	// TODO [eventually]: handle time metadata in new UFS format standard

	return &attr, nil
}

func unixfsTypeToCoreType(ut unixpb.Data_DataType) coreiface.FileType {
	switch ut {
	case unixpb.Data_Directory, unixpb.Data_HAMTShard:
		return coreiface.TDirectory
	case unixpb.Data_Symlink:
		return coreiface.TSymlink
	case unixpb.Data_File, unixpb.Data_Raw:
		return coreiface.TFile
	default:
		return coreiface.TUnknown
	}
}

// TODO: Name "existing" is weird.
// It's the stat we'll already have to do type checking during Open operations.
// It may or may not be used at all.
func (ci *coreInterface) genStatFunc(name string, existing *stat) statFunc {
	if ci.systemID == filesystem.IPFS {
		// These files should be permanent by IPFS specification.
		// When requested, always return a static stat object.
		return func() (fs.FileInfo, error) { return existing, nil }
	} else {
		// These files may change during operation.
		// When requested, dynamically generate a new stat object.
		return func() (fs.FileInfo, error) { return ci.Stat(name) }
	}
}
