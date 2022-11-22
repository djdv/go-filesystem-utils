//go:build !nofuse

package cgofuse

import (
	"io/fs"
	"path/filepath"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/u-root/uio/ulog"
	"github.com/winfsp/cgofuse/fuse"
)

const (
	goRoot = "."
)

type goWrapper struct {
	*fileTable
	systemLock operationsLock
	fs.FS
	log ulog.Logger
}

func (fs *goWrapper) Init() {
	fs.log.Print("Init")
	// TODO should we initalize these here
	// or in the constructor?
	// The latter might make more sense if we need to share locks
	// across APIs.
	// I.e. we shouldn't lock at the fuse level, but at the fs.FS level
	// so that the same system accessed from FUSE, 9P, Go, etc.
	// can't collide.
	fs.fileTable = newFileTable()
	fs.systemLock = newOperationsLock()
}

func (fs *goWrapper) Destroy() {
	// TODO: dbg lint
	fs.log.Print("Destroy")
	defer fs.log.Print("Destroy finished")
	/* TODO: something like this for the new system
	tell the Go FS we're leaving, which itself should have some reference counter.
	we also need to track and close our handles again.
	Old code:
	defer func() {
		if fs.destroySignal != nil {
			// TODO: close all file/dir indices, stream errors out to destroy chan
			close(fs.destroySignal)
			fs.destroySignal = nil
		}
		fs.log.Debugf("Destroy finished")
	}()
	*/

	if err := fs.fileTable.Close(); err != nil {
		fs.log.Print("failed to close:", err)
	}
}

func (fs *goWrapper) Flush(path string, fh uint64) int {
	fs.log.Printf("Flush {%X}%q", fh, path)
	return -fuse.ENOSYS
}

func (fs *goWrapper) Access(path string, mask uint32) int {
	fs.log.Printf("Access {%X}%q", mask, path)
	return -fuse.ENOSYS
}

func (fs *goWrapper) Chown(path string, uid, gid uint32) int {
	fs.log.Printf("Chown {%d|%d}%q", uid, gid, path)
	defer fs.systemLock.Modify(path)()
	return -fuse.ENOSYS
}

func (gw *goWrapper) Rename(oldpath, newpath string) int {
	/*
		fs.log.Warnf("Rename - HostRequest %q->%q", oldpath, newpath)
		if err := fs.nodeInterface.Rename(oldpath, newpath); err != nil {
			fs.log.Error(err)
			return interpretError(err)
		}
		return operationSuccess
	*/

	// TODO: properly migrate this to filesystem pkg; needs name and that
	// ported from the pre-FS.fs code.
	if renamer, ok := gw.FS.(filesystem.RenameFS); ok {
		goOldPath, err := fuseToGo(oldpath)
		if err != nil {
			return interpretError(err)
		}
		goNewPath, err := fuseToGo(newpath)
		if err != nil {
			return interpretError(err)
		}
		if err := renamer.Rename(goOldPath, goNewPath); err != nil {
			return interpretError(err)
		}
		return operationSuccess
	}
	return -fuse.ENOSYS
}

func (fs *goWrapper) Link(oldpath, newpath string) int {
	fs.log.Printf("Link %q<->%q", oldpath, newpath)
	return -fuse.ENOSYS
}

func (gw *goWrapper) Unlink(path string) int {
	defer gw.systemLock.CreateOrDelete(path)()
	/*
		gw.log.Debugf("Unlink - HostRequest %q", path)
		if path == "/" {
			gw.log.Error(fuselib.Error(-fuselib.EPERM))
			return -fuselib.EPERM
		}
		if err := gw.nodeInterface.Remove(path); err != nil {
			gw.log.Error(err)
			return interpretError(err)
		}
		return operationSuccess
	*/
	// TODO: properly migrate this to filesystem pkg; needs name and that
	// ported from the pre-FS.fs code.
	if remover, ok := gw.FS.(filesystem.RemoveFS); ok {
		goPath, err := fuseToGo(path)
		if err != nil {
			return interpretError(err)
		}
		if err := remover.Remove(goPath); err != nil {
			return interpretError(err)
		}
		return operationSuccess
	}
	return -fuse.ENOSYS
}

func (gw *goWrapper) Symlink(target, newpath string) int {
	//defer gw.systemLock.Modify(path)()
	/*
		fs.log.Debugf("Symlink - HostRequest %q->%q", newpath, target)

		if err := fs.nodeInterface.MakeLink(target, newpath); err != nil {
			fs.log.Error(err)
			return interpretError(err)
		}

		return operationSuccess
	*/

	// TODO: properly migrate this to filesystem pkg; needs name and that
	// ported from the pre-FS.fs code.
	if maker, ok := gw.FS.(filesystem.SymlinkFS); ok {
		goTarget, err := fuseToGo(target)
		if err != nil {
			return interpretError(err)
		}
		goNewPath, err := fuseToGo(newpath)
		if err != nil {
			return interpretError(err)
		}
		if err := maker.MakeLink(goTarget, goNewPath); err != nil {
			return interpretError(err)
		}
		return operationSuccess
	}
	return -fuse.ENOSYS
}

func (gw *goWrapper) Readlink(path string) (int, string) {
	/*
		fs.log.Debugf("Readlink - %q", path)
		switch path {
		case "/":
			fs.log.Warnf("Readlink - root path is an invalid Request")
			return -fuselib.EINVAL, ""
		case "":
			fs.log.Error("Readlink - empty Request")
			return -fuselib.ENOENT, ""
		}
		linkString, err := fs.nodeInterface.ExtractLink(path)
		if err != nil {
			fs.log.Error(err)
			return interpretError(err), ""
		}
		// NOTE: paths returned here get sent back to the FUSE library
		// they should not be native paths, regardless of their source format
		return operationSuccess, filepath.ToSlash(linkString)
	*/
	switch path {
	case "/":
		// fs.log.Printf("Readlink - root path is an invalid Request")
		return -fuse.EINVAL, ""
	case "":
		// fs.log.Print("Readlink - empty Request")
		return -fuse.ENOENT, ""
	default:
		if extractor, ok := gw.FS.(filesystem.SymlinkFS); ok {
			goPath, err := fuseToGo(path)
			if err != nil {
				return interpretError(err), ""
			}
			linkString, err := extractor.ExtractLink(goPath)
			if err != nil {
				// fs.log.Error(err)
				return interpretError(err), ""
			}
			return operationSuccess, filepath.ToSlash(linkString)
		}
		return -fuse.ENOSYS, ""
	}
}

func (fs *goWrapper) Chmod(path string, mode uint32) int {
	fs.log.Printf("Chmod {%X}%q", mode, path)
	defer fs.systemLock.Modify(path)()
	return -fuse.ENOSYS
}
