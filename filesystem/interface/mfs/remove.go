package mfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	gopath "path"

	iferrors "github.com/ipfs/go-ipfs/filesystem/interface/errors"
	gomfs "github.com/ipfs/go-mfs"
	"github.com/ipfs/go-unixfs"
	unixpb "github.com/ipfs/go-unixfs/pb"
)

func (mi *mfsInterface) Remove(path string) error {
	return mi.remove(path, gomfs.TFile)
}

func (mi *mfsInterface) RemoveLink(path string) error {
	return mi.remove(path, gomfs.TFile) // TODO: this is a gross hack; change the parameter to be a core type and switch on it properly inside remove
}

func (mi *mfsInterface) RemoveDirectory(path string) error {
	return mi.remove(path, gomfs.TDir)
}

func (mi *mfsInterface) remove(path string, nodeType gomfs.NodeType) error {
	// prepare to separate child from parent
	parentDir, childName, err := splitParentChild(mi.mroot, path)
	if err != nil {
		return err
	}

	childNode, err := parentDir.Child(childName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return iferrors.NotExist(path)
		}
		return iferrors.Other(path, err)
	}

	// compare MFS node type with the request type
	switch nodeType {
	case gomfs.TFile:
		// MFS does not expose link metadata via nodeType
		// so this is how we distinguish link requests
		// if we can assert the MFS node as an mfs file, then it's treated as a regular file
		if gomfs.IsFile(childNode) {
			break
		}
		// otherwise we deduce that it's a symlink, since it's not a directory request
		// and the node is not a file
		ipldNode, err := childNode.GetNode()
		if err != nil {
			return iferrors.Permission(path, err)
		}
		// extract the type from Unix FS directly, bypassing MFS
		ufsNode, err := unixfs.ExtractFSNode(ipldNode)
		if err != nil {
			return iferrors.Permission(path, err)
		}
		if t := ufsNode.Type(); t != unixpb.Data_Symlink {
			// UFS says this node is not a symlink; bail out with posix error
			return fmt.Errorf("(Type: %v), %w",
				t,
				iferrors.IsDir(path),
			)
		}

	case gomfs.TDir:
		childDir, ok := childNode.(*gomfs.Directory)
		if !ok {
			return fmt.Errorf("(Type: %v), %w",
				childNode.Type(),
				iferrors.NotDir(path),
			)
		}

		ents, err := childDir.ListNames(context.TODO())
		if err != nil {
			return iferrors.Permission(path, err)
		}

		if len(ents) != 0 {
			return iferrors.NotEmpty(path)
		}

	default:
		return iferrors.Permission(path,
			fmt.Errorf("unexpected node type: %v", nodeType))
	}

	// unlink parent and child actually
	if err := parentDir.Unlink(childName); err != nil {
		return iferrors.Permission(path, err)
	}
	if err := parentDir.Flush(); err != nil {
		return iferrors.Permission(path, err)
	}

	return nil
}

func splitParentChild(mroot *gomfs.Root, path string) (*gomfs.Directory, string, error) {
	parentPath, childName := gopath.Split(path)
	parentNode, err := gomfs.Lookup(mroot, parentPath)
	if err != nil {
		return nil, "", mfsLookupErr(parentPath, err)
	}

	parentDir, ok := parentNode.(*gomfs.Directory)
	if !ok {
		return nil, "", iferrors.NotDir(parentPath)
	}

	return parentDir, childName, nil
}
