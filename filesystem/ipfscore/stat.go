package ipfs

import (
	goerrors "errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/filesystem/errors"
	cmds "github.com/ipfs/go-ipfs-cmds"
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

	stat, err := ci.stat(name, nil)
	if err != nil {
		// TODO: if the cmds lib doesn't have a typed error we can use with .Is
		// one should be added for this. Checking messages like this is not stable.
		cmdsErr := new(cmds.Error)
		if goerrors.As(err, &cmdsErr) &&
			strings.Contains(cmdsErr.Message, "no link named") {
			return nil, errors.New(errors.NotExist, err)
		}

		return nil, errors.New(op,
			errors.Path(name),
			errors.IO, // TODO: [review] double check this Kind makes sense for this.
			err,
		)
	}
	return stat, nil
}

// TODO: reconsider if there's a better way to split up functions to
// handle calling stat from other calls which may or may not already have a resolved node.
// (Open+OpenDir will need one anyway; Stat itself won't)
// This whole callchain looks too C-like and I hate it. But this might be how it has to be.
// Otherwise callers will need to do too much manual data insertions on the stat struct
// which is bound to cause inconsistencies (which are VERY BAD for the OS
// and make debugging confusing)
//
// ipldNode is optional.
func (ci *coreInterface) stat(name string, ipldNode ipld.Node) (*stat, error) {
	if ipldNode == nil {
		var (
			err      error
			ipfsPath = goToIPFSCore(ci.systemID, name)
		)
		if ipldNode, err = resolveNode(ci.ctx, ci.core, ipfsPath); err != nil {
			return nil, err
		}
	}

	stat := new(stat)
	if err := statNode(ipldNode, stat); err != nil {
		return nil, err
	}
	stat.crtime = ci.creationTime
	stat.name = path.Base(name)
	return stat, nil
}

// write: populates possible fields within stat, from node data.
func statNode(ipldNode ipld.Node, stat *stat) error {
	if typedNode, ok := ipldNode.(*dag.ProtoNode); ok {
		ufsNode, err := unixfs.ExtractFSNode(typedNode)
		if err != nil {
			return err
		}
		return unixFSAttr(ufsNode, stat)
	}
	//  *dag.RawNode, *cbor.Node
	return genericAttr(ipldNode, stat)
}

func genericAttr(genericNode ipld.Node, attr *stat) error {
	// raw nodes only contain data so we'll treat them as a flat file
	// cbor nodes are not currently supported via UnixFS so we assume them to contain only data
	// TODO: review ^ is there some way we can implement this that won't blow up in the future?
	// (if unixfs supports cbor and directories are implemented to use them )
	attr.typ = coreiface.TFile

	nodeStat, err := genericNode.Stat()
	if err != nil {
		return err
	}

	attr.size = uint64(nodeStat.CumulativeSize)
	attr.blockSize = uint64(nodeStat.BlockSize)

	return nil
}

func unixFSAttr(ufsNode *unixfs.FSNode, attr *stat) error {
	attr.typ = unixfsTypeToCoreType(ufsNode.Type())

	blocks := len(ufsNode.BlockSizes())
	if blocks > 0 {
		attr.blockSize = ufsNode.BlockSize(0)
		attr.blocks = uint64(blocks)
	}

	attr.size = ufsNode.FileSize()

	// TODO [eventually]: handle time metadata in new UFS format standard

	return nil
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
