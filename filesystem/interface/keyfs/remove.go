package keyfs

import (
	"github.com/ipfs/go-ipfs/filesystem"
	interfaceutils "github.com/ipfs/go-ipfs/filesystem/interface"
	iferrors "github.com/ipfs/go-ipfs/filesystem/interface/errors"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

func (ki *keyInterface) Remove(path string) error {
	fs, _, fsPath, deferFunc, err := ki.selectFS(path)
	if err != nil {
		return err
	}
	defer deferFunc()

	if fs == ki {
		return ki.remove(fsPath, coreiface.TFile)
	}
	return fs.Remove(fsPath)
}

func (ki *keyInterface) RemoveLink(path string) error {
	fs, _, fsPath, deferFunc, err := ki.selectFS(path)
	if err != nil {
		return err
	}
	defer deferFunc()

	if fs == ki {
		return ki.remove(fsPath, coreiface.TSymlink)
	}
	return fs.RemoveLink(fsPath)
}

func (ki *keyInterface) RemoveDirectory(path string) error {
	fs, _, fsPath, deferFunc, err := ki.selectFS(path)
	if err != nil {
		return err
	}
	defer deferFunc()

	if fs == ki {
		return ki.remove(fsPath, coreiface.TDirectory)
	}
	return fs.RemoveDirectory(fsPath)
}

func (ki *keyInterface) remove(path string, requestType coreiface.FileType) error {
	nodeMeta, _, err := ki.Info(path, filesystem.StatRequest{Type: true})
	if err != nil {
		return err
	}

	if nodeMeta.Type != requestType {
		switch requestType {
		case coreiface.TFile:
			return iferrors.IsDir(path)
		case coreiface.TDirectory:
			return iferrors.NotDir(path)
		case coreiface.TSymlink:
			// TODO: [review] SUS doesn't distinguish between files and links in `unlink`
			// so there's no real appropriate value for this
			return iferrors.NotExist(path)
		}
	}

	callCtx, cancel := interfaceutils.CallContext(ki.ctx)
	defer cancel()
	keyName := path[1:]
	if _, err = ki.core.Key().Remove(callCtx, keyName); err != nil {
		return iferrors.IO(path, err)
	}
	return nil
}
