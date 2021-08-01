package mfs

import (
	"errors"
	"fmt"
	"os"

	"github.com/ipfs/go-ipfs/filesystem"
	iferrors "github.com/ipfs/go-ipfs/filesystem/interface/errors"
	gomfs "github.com/ipfs/go-mfs"
	"github.com/ipfs/go-unixfs"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

func (mi *mfsInterface) Info(path string, req filesystem.StatRequest) (*filesystem.Stat, filesystem.StatRequest, error) {
	var (
		attr   = new(filesystem.Stat)
		filled filesystem.StatRequest
	)

	mfsNode, err := gomfs.Lookup(mi.mroot, path)
	if err != nil {
		return attr, filled, mfsLookupErr(path, err)
	}

	ipldNode, err := mfsNode.GetNode()
	if err != nil {
		return attr, filled, iferrors.Other(path, err)
	}

	ufsNode, err := unixfs.ExtractFSNode(ipldNode)
	if err != nil {
		return attr, filled, iferrors.Other(path, err)
	}

	if req.Type {
		switch mfsNode.Type() {
		case gomfs.TFile:
			attr.Type, filled.Type = coreiface.TFile, true
		case gomfs.TDir:
			attr.Type, filled.Type = coreiface.TDirectory, true
		default:
			// symlinks are not natively supported by MFS / the Files API but we support them
			nodeType := ufsNode.Type()
			if nodeType == unixfs.TSymlink {
				attr.Type, filled.Type = coreiface.TSymlink, true
				break
			}

			return attr, filled, iferrors.Other(path,
				fmt.Errorf("unexpected node type %d", nodeType),
			)
		}
	}

	if req.Size {
		attr.Size, filled.Size = ufsNode.FileSize(), true
	}

	if req.Blocks && !filled.Blocks {
		// NOTE: we can't account for variable block size so we use the size of the first block only (if any)
		blocks := len(ufsNode.BlockSizes())
		if blocks > 0 {
			attr.BlockSize = ufsNode.BlockSize(0)
			attr.Blocks = uint64(blocks)
		}

		// 0 is a valid value for these fields, especially for non-regular files
		// so set this to true regardless of if one was provided or not
		filled.Blocks = true
	}

	return attr, filled, nil
}

func (mi *mfsInterface) ExtractLink(path string) (string, error) {
	mfsNode, err := gomfs.Lookup(mi.mroot, path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", iferrors.NotExist(path)
		}
		// TODO: SUS annotation; error deviates from file/dir standard
		// TODO: ^ this is likely wrong actually; sus says "file"
		// which may be the generic use, not implying "regular file"
		// we need to inspect the value a compliant system returns, it's probably ENOENT, not EINVAL
		err := errors.New("invalid link request")
		return "", iferrors.Permission(path, err)
	}

	ipldNode, err := mfsNode.GetNode()
	if err != nil {
		return "", iferrors.IO(path, err)
	}

	ufsNode, err := unixfs.ExtractFSNode(ipldNode)
	if err != nil {
		return "", iferrors.IO(path, err)
	}
	if ufsNode.Type() != unixfs.TSymlink {
		err := fmt.Errorf("type %v is not a link", ufsNode.Type())
		return "", iferrors.UnsupportedItem(path, err)
	}

	return string(ufsNode.Data()), nil
}
