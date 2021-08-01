package interfaceutils

import (
	"context"
	"fmt"
	"time"

	files "github.com/ipfs/go-ipfs-files"
	"github.com/ipfs/go-ipfs/filesystem"
	iferrors "github.com/ipfs/go-ipfs/filesystem/interface/errors"
	ipld "github.com/ipfs/go-ipld-format"
	dag "github.com/ipfs/go-merkledag"
	"github.com/ipfs/go-unixfs"
	unixpb "github.com/ipfs/go-unixfs/pb"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	corepath "github.com/ipfs/interface-go-ipfs-core/path"
)

const callTimeout = 20 * time.Second

// CallContext provides a standard context
// to be used during file system operation calls that make short lived calls to functions which
// take a context. For example, `CoreAPI.ResolveNode`
// But not for long lived operations such as a hypothetical `File.Open(ctx)`.
func CallContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, callTimeout)
}

// TODO: docs
type CoreExtender interface {
	coreiface.CoreAPI
	// Stat takes in a path and a list of desired attributes for the object residing at that path
	// Along with the container of values,
	// it returns a list of attributes which were populated
	// Stat is not guaranteed to return the request exactly
	// it may contain more or less information than was requested
	// Thus, it is the callers responsibility to inspect the returned list
	// to see if values they require were in fact populated
	// (this is due to the fact that the referenced objects
	// may not implement the constructs requested)
	Stat(context.Context, corepath.Path, filesystem.StatRequest) (*filesystem.Stat, filesystem.StatRequest, error)
	// ExtractLink takes in a path to a link and returns the string it contains
	ExtractLink(corepath.Path) (string, error)
}

// TODO: docs
type CoreExtended struct{ coreiface.CoreAPI }

// TODO: docs
func (core *CoreExtended) Stat(ctx context.Context, path corepath.Path, req filesystem.StatRequest) (*filesystem.Stat, filesystem.StatRequest, error) {
	ipldNode, err := core.ResolveNode(ctx, path)
	if err != nil {
		return nil, filesystem.StatRequest{}, err
	}

	switch typedNode := ipldNode.(type) {
	case *dag.ProtoNode:
		ufsNode, err := unixfs.ExtractFSNode(typedNode)
		if err != nil {
			return nil, filesystem.StatRequest{}, iferrors.Other(path.String(), err)
		}
		return unixFSAttr(ufsNode, req)

	// pretend Go allows this:
	// case *dag.RawNode, *cbor.Node:
	// fallthrough
	default:
		return genericAttr(typedNode, req)
	}
}

// ExtractLink takes in a path to a UFS symlink, and returns its target.
func (core *CoreExtended) ExtractLink(path corepath.Path) (string, error) {
	// make sure the path is actually a link
	callCtx, cancel := CallContext(context.Background())
	defer cancel()
	iStat, _, err := core.Stat(callCtx, path, filesystem.StatRequest{Type: true})
	if err != nil {
		return "", err
	}

	if iStat.Type != coreiface.TSymlink {
		return "", iferrors.UnsupportedItem(path.String(),
			fmt.Errorf("%q is not a symlink", path.String()),
		)
	}

	// if it is, read it
	linkNode, err := core.Unixfs().Get(callCtx, path)
	if err != nil {
		return "", iferrors.IO(path.String(), err)
	}

	// NOTE: the implementation of this does no type checks [2020.06.04]
	// which is why we check the node's type above
	return files.ToSymlink(linkNode).Target, nil
}

// ResolveNode wraps the core method, but uses our error type for the return.
func (core *CoreExtended) ResolveNode(ctx context.Context, path corepath.Path) (ipld.Node, error) {
	n, err := core.CoreAPI.ResolveNode(ctx, path)
	if err != nil {
		// TODO: inspect error to disambiguate type
		return nil, iferrors.NotExist(path.String())
	}
	return n, nil
}

// ResolvePath wraps the core method, but uses our error type for the return.
func (core *CoreExtended) ResolvePath(ctx context.Context, path corepath.Path) (corepath.Resolved, error) {
	p, err := core.CoreAPI.ResolvePath(ctx, path)
	if err != nil {
		// TODO: inspect error to disambiguate type
		return nil, iferrors.NotExist(path.String())
	}
	return p, nil
}

func genericAttr(genericNode ipld.Node, req filesystem.StatRequest) (*filesystem.Stat, filesystem.StatRequest, error) {
	var (
		attr        = new(filesystem.Stat)
		filledAttrs filesystem.StatRequest
	)

	if req.Type {
		// raw nodes only contain data so we'll treat them as a flat file
		// cbor nodes are not currently supported via UnixFS so we assume them to contain only data
		// TODO: review ^ is there some way we can implement this that won't blow up in the future?
		// (if unixfs supports cbor and directories are implemented to use them )
		attr.Type, filledAttrs.Type = coreiface.TFile, true
	}

	if req.Size || req.Blocks {
		nodeStat, err := genericNode.Stat()
		if err != nil {
			return attr, filledAttrs, iferrors.IO(genericNode.String(), err)
		}

		if req.Size {
			attr.Size, filledAttrs.Size = uint64(nodeStat.CumulativeSize), true
		}

		if req.Blocks {
			attr.BlockSize, filledAttrs.Blocks = uint64(nodeStat.BlockSize), true
		}
	}

	return attr, filledAttrs, nil
}

// returns attr, filled members, error.
func unixFSAttr(ufsNode *unixfs.FSNode, req filesystem.StatRequest) (*filesystem.Stat, filesystem.StatRequest, error) {
	var (
		attr        filesystem.Stat
		filledAttrs filesystem.StatRequest
	)

	if req.Type {
		attr.Type, filledAttrs.Type = unixfsTypeToCoreType(ufsNode.Type()), true
	}

	if req.Blocks {
		// NOTE: we can't account for variable block size so we use the size of the first block only (if any)
		blocks := len(ufsNode.BlockSizes())
		if blocks > 0 {
			attr.BlockSize = ufsNode.BlockSize(0)
			attr.Blocks = uint64(blocks)
		}

		// 0 is a valid value for these fields, especially for non-regular files
		// so set this to true regardless of if one was provided or not
		filledAttrs.Blocks = true
	}

	if req.Size {
		attr.Size, filledAttrs.Size = ufsNode.FileSize(), true
	}

	// TODO [eventually]: handle time metadata in new UFS format standard

	return &attr, filledAttrs, nil
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
