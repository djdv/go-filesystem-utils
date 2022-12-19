//go:build !nofuse

package cgofuse

import (
	"io/fs"
	"path"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse/lock"
	"github.com/u-root/uio/ulog"
	"github.com/winfsp/cgofuse/fuse"
)

type goWrapper struct {
	fs.FS
	log ulog.Logger
	*fileTable
	systemLock   lock.PathLocker
	activeMounts uint64
}

func (gw *goWrapper) Init() {
	defer gw.systemLock.CreateOrDelete(posixRoot)()
	gw.activeMounts++
	if gw.fileTable == nil {
		gw.fileTable = newFileTable()
	}
}

func (gw *goWrapper) Destroy() {
	defer gw.systemLock.CreateOrDelete(posixRoot)()
	// TODO: errors here need to be ferried
	// to the constructor caller (optionally?),
	// their responsibility to handle.
	if gw.activeMounts--; gw.activeMounts == 0 {
		if err := gw.fileTable.Close(); err != nil {
			gw.logError(posixRoot, err)
		}
		gw.fileTable = nil
	}
}

func (gw *goWrapper) Flush(path string, fh fileDescriptor) errNo {
	defer gw.systemLock.Modify(path)()
	return -fuse.ENOSYS
}

func (gw *goWrapper) Access(path string, mask uint32) errNo {
	defer gw.systemLock.Access(path)()
	return -fuse.ENOSYS
}

func (gw *goWrapper) Chown(path string, uid, gid uint32) errNo {
	defer gw.systemLock.Modify(path)()
	return -fuse.ENOSYS
}

func (gw *goWrapper) Rename(oldpath, newpath string) errNo {
	if path.Dir(oldpath) == path.Dir(newpath) {
		defer gw.systemLock.Rename(oldpath, newpath)()
	} else {
		defer gw.systemLock.Move(oldpath, newpath)()
	}
	if renamer, ok := gw.FS.(filesystem.RenameFS); ok {
		goOldPath, goNewPath, err := fuseToGoPair(oldpath, newpath)
		if err != nil {
			gw.logError(oldpath+"->"+newpath, err)
			return interpretError(err)
		}
		if err := renamer.Rename(goOldPath, goNewPath); err != nil {
			gw.logError(oldpath+"->"+newpath, err)
			return interpretError(err)
		}
		return operationSuccess
	}
	return -fuse.ENOSYS
}

func (gw *goWrapper) Link(oldpath, newpath string) errNo {
	if path.Dir(oldpath) == path.Dir(newpath) {
		defer gw.systemLock.Rename(oldpath, newpath)()
	} else {
		defer gw.systemLock.Move(oldpath, newpath)()
	}
	return -fuse.ENOSYS
}

func (gw *goWrapper) Unlink(path string) errNo {
	defer gw.systemLock.CreateOrDelete(path)()
	if remover, ok := gw.FS.(filesystem.RemoveFS); ok {
		goPath, err := fuseToGo(path)
		if err != nil {
			gw.logError(path, err)
			return interpretError(err)
		}
		if err := remover.Remove(goPath); err != nil {
			gw.logError(path, err)
			return interpretError(err)
		}
		return operationSuccess
	}
	return -fuse.ENOSYS
}

func (gw *goWrapper) Symlink(target, newpath string) errNo {
	defer gw.systemLock.CreateOrDelete(newpath)()
	if linker, ok := gw.FS.(filesystem.SymlinkFS); ok {
		goTarget, goNewPath, err := fuseToGoPair(target, newpath)
		if err != nil {
			gw.logError(newpath+"->"+target, err)
			return interpretError(err)
		}
		if err := linker.Symlink(goTarget, goNewPath); err != nil {
			gw.logError(newpath+"->"+target, err)
			return interpretError(err)
		}
		return operationSuccess
	}
	return -fuse.ENOSYS
}

func (gw *goWrapper) Readlink(path string) (errNo, string) {
	defer gw.systemLock.Access(path)()
	switch path {
	case "/":
		return -fuse.EINVAL, ""
	case "":
		return -fuse.ENOENT, ""
	default:
		if extractor, ok := gw.FS.(filesystem.SymlinkFS); ok {
			goPath, err := fuseToGo(path)
			if err != nil {
				gw.logError(path, err)
				return interpretError(err), ""
			}
			fsLink, err := extractor.Readlink(goPath)
			if err != nil {
				gw.logError(path, err)
				return interpretError(err), ""
			}
			fuseLink := posixRoot + fsLink
			return operationSuccess, fuseLink
		}
		return -fuse.ENOSYS, ""
	}
}

func (gw *goWrapper) Chmod(path string, mode uint32) errNo {
	defer gw.systemLock.Modify(path)()
	return -fuse.ENOSYS
}

func (gw *goWrapper) logError(path string, err error) {
	gw.log.Printf(`"%s" - %s`, path, err)
}
