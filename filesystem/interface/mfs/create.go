package mfs

import (
	"errors"
	"os"
	gopath "path"

	iferrors "github.com/ipfs/go-ipfs/filesystem/interface/errors"
	dag "github.com/ipfs/go-merkledag"
	gomfs "github.com/ipfs/go-mfs"
	"github.com/ipfs/go-unixfs"
)

func (mi *mfsInterface) Make(path string) error {
	parentPath, childName := gopath.Split(path)
	parentNode, err := gomfs.Lookup(mi.mroot, parentPath)
	if err != nil {
		return mfsLookupErr(parentPath, err)
	}

	parentDir, ok := parentNode.(*gomfs.Directory)
	if !ok {
		return iferrors.NotDir(parentPath)
	}

	if _, err := parentDir.Child(childName); err == nil {
		return iferrors.Exist(path)
	}

	dagFile := dag.NodeWithData(unixfs.FilePBData(nil, 0))
	dagFile.SetCidBuilder(parentDir.GetCidBuilder())
	if err := parentDir.AddChild(childName, dagFile); err != nil {
		return iferrors.IO(path, err)
	}

	return nil
}

func (mi *mfsInterface) MakeDirectory(path string) error {
	if err := gomfs.Mkdir(mi.mroot, path, gomfs.MkdirOpts{Flush: true}); err != nil {
		if errors.Is(err, os.ErrExist) {
			return iferrors.Exist(path)
		}

		return iferrors.Permission(path, err)
	}

	return nil
}

func (mi *mfsInterface) MakeLink(path, linkTarget string) error {
	parentPath, linkName := gopath.Split(path)

	parentNode, err := gomfs.Lookup(mi.mroot, parentPath)
	if err != nil {
		return mfsLookupErr(parentPath, err)
	}

	parentDir, ok := parentNode.(*gomfs.Directory)
	if !ok {
		return iferrors.NotDir(parentPath)
	}

	if _, err := parentDir.Child(linkName); err == nil {
		return iferrors.Exist(path)
	}

	dagData, err := unixfs.SymlinkData(linkTarget)
	if err != nil {
		// TODO: SUS annotation
		return iferrors.NotExist(linkName)
	}

	// TODO: same note as on keyfs; use raw node's for this if we can
	dagNode := dag.NodeWithData(dagData)
	dagNode.SetCidBuilder(parentDir.GetCidBuilder())

	if err := parentDir.AddChild(linkName, dagNode); err != nil {
		// SUSv7
		// "...or write permission is denied on the parent directory of the directory to be created"
		return iferrors.NotExist(linkName)
	}
	return nil
}
