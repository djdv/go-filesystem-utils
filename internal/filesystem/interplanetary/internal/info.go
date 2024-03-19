package interplanetary

import (
	"io/fs"
	"time"

	mdag "github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs"
	unixpb "github.com/ipfs/boxo/ipld/unixfs/pb"
	cbor "github.com/ipfs/go-ipld-cbor"
	ipld "github.com/ipfs/go-ipld-format"
)

type (
	NodeInfo struct {
		ModTime_ time.Time
		Name_    string
		Size_    int64
		Mode_    fs.FileMode
	}
	StatFunc func(fs.FS, string) (fs.FileInfo, error)
)

func (ni *NodeInfo) Name() string       { return ni.Name_ }
func (ni *NodeInfo) Size() int64        { return ni.Size_ }
func (ni *NodeInfo) Mode() fs.FileMode  { return ni.Mode_ }
func (ni *NodeInfo) ModTime() time.Time { return ni.ModTime_ }
func (ni *NodeInfo) IsDir() bool        { return ni.Mode().IsDir() }
func (ni *NodeInfo) Sys() any           { return ni }

func StatNode(node ipld.Node, info *NodeInfo) error {
	switch typedNode := node.(type) {
	case *mdag.ProtoNode:
		return statProto(typedNode, info)
	case *cbor.Node:
		return statCbor(typedNode, info)
	default:
		return statGeneric(node, info)
	}
}

func statProto(node *mdag.ProtoNode, info *NodeInfo) error {
	ufsNode, err := unixfs.ExtractFSNode(node)
	if err != nil {
		return err
	}
	info.Size_ = int64(ufsNode.FileSize())
	switch ufsNode.Type() {
	case unixpb.Data_Directory, unixpb.Data_HAMTShard:
		info.Mode_ |= fs.ModeDir
	case unixpb.Data_Symlink:
		info.Mode_ |= fs.ModeSymlink
	case unixpb.Data_File, unixpb.Data_Raw:
	// NOOP:  stat.mode |= fs.FileMode(0)
	default:
		info.Mode_ |= fs.ModeIrregular
	}
	return nil
}

func statCbor(node *cbor.Node, info *NodeInfo) error {
	size, err := node.Size()
	if err != nil {
		return err
	}
	info.Size_ = int64(size)
	return nil
}

func statGeneric(node ipld.Node, info *NodeInfo) error {
	nodeStat, err := node.Stat()
	if err != nil {
		return err
	}
	info.Size_ = int64(nodeStat.CumulativeSize)
	return nil
}

func FSTypeName(mode fs.FileMode) string {
	switch mode.Type() {
	case fs.FileMode(0):
		return "regular"
	case fs.ModeDir:
		return "directory"
	case fs.ModeSymlink:
		return "symbolic link"
	case fs.ModeNamedPipe:
		return "named pipe"
	case fs.ModeSocket:
		return "socket"
	case fs.ModeDevice:
		return "device"
	case fs.ModeCharDevice:
		return "character device"
	case fs.ModeIrregular:
		fallthrough
	default:
		return "irregular"
	}
}
