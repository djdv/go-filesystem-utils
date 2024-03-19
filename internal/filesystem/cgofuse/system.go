package cgofuse

import (
	"io/fs"
	"path"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse/lock"
	"github.com/u-root/uio/ulog"
	"github.com/winfsp/cgofuse/fuse"
)

type fileSystem struct {
	mountID string
	fs.FS
	log ulog.Logger
	*fileTable
	systemLock   lock.PathLocker
	deleteAccess []string
	activeMounts uint64
}

const (
	// Host defines the identifier of this system.
	Host filesystem.Host = "FUSE"

	posixRoot        = "/"
	operationSuccess = 0
)

func (fsys *fileSystem) logError(path string, err error) {
	const logFmt = `"%s" - %s`
	if joinErrs, ok := err.(interface {
		Unwrap() []error
	}); ok {
		for _, err := range joinErrs.Unwrap() {
			fsys.log.Printf(logFmt, path, err)
		}
	} else {
		fsys.log.Printf(logFmt, path, err)
	}
}

func (fsys *fileSystem) Init() {
	defer fsys.systemLock.CreateOrDelete(posixRoot)()
	fsys.activeMounts++
	if fsys.fileTable == nil {
		fsys.fileTable = newFileTable()
	}
}

func (fsys *fileSystem) Destroy() {
	defer fsys.systemLock.CreateOrDelete(posixRoot)()
	// TODO: errors here need to be ferried
	// to the constructor caller (optionally?),
	// their responsibility to handle.
	if fsys.activeMounts--; fsys.activeMounts == 0 {
		if err := fsys.fileTable.Close(); err != nil {
			fsys.logError(posixRoot, err)
		}
		fsys.fileTable = nil
	}
	if err := filesystem.Close(fsys.FS); err != nil {
		fsys.logError(posixRoot, err)
	}
}

func (fsys *fileSystem) Link(oldpath, newpath string) errNo {
	if path.Dir(oldpath) == path.Dir(newpath) {
		defer fsys.systemLock.Rename(oldpath, newpath)()
	} else {
		defer fsys.systemLock.Move(oldpath, newpath)()
	}
	return -fuse.ENOSYS
}

func (fsys *fileSystem) Unlink(path string) errNo {
	defer fsys.systemLock.CreateOrDelete(path)()
	if remover, ok := fsys.FS.(filesystem.RemoveFS); ok {
		goPath, err := fuseToGo(path)
		if err != nil {
			fsys.logError(path, err)
			return interpretError(err)
		}
		if err := remover.Remove(goPath); err != nil {
			fsys.logError(path, err)
			return interpretError(err)
		}
		return operationSuccess
	}
	return -fuse.ENOSYS
}

func (fsys *fileSystem) Rename(oldpath, newpath string) errNo {
	if path.Dir(oldpath) == path.Dir(newpath) {
		defer fsys.systemLock.Rename(oldpath, newpath)()
	} else {
		defer fsys.systemLock.Move(oldpath, newpath)()
	}
	if renamer, ok := fsys.FS.(filesystem.RenameFS); ok {
		goOldPath, goNewPath, err := fuseToGoPair(oldpath, newpath)
		if err != nil {
			fsys.logError(oldpath+"->"+newpath, err)
			return interpretError(err)
		}
		if err := renamer.Rename(goOldPath, goNewPath); err != nil {
			fsys.logError(oldpath+"->"+newpath, err)
			return interpretError(err)
		}
		return operationSuccess
	}
	return -fuse.ENOSYS
}

func (fsys *fileSystem) Symlink(target, newpath string) errNo {
	defer fsys.systemLock.CreateOrDelete(newpath)()
	if linker, ok := fsys.FS.(filesystem.WritableSymlinkFS); ok {
		goTarget, goNewPath, err := fuseToGoPair(target, newpath)
		if err != nil {
			fsys.logError(newpath+"->"+target, err)
			return interpretError(err)
		}
		if err := linker.Symlink(goTarget, goNewPath); err != nil {
			fsys.logError(newpath+"->"+target, err)
			return interpretError(err)
		}
		return operationSuccess
	}
	return -fuse.ENOSYS
}

func (fsys *fileSystem) Readlink(path string) (errNo, string) {
	defer fsys.systemLock.Access(path)()
	switch path {
	case "/":
		return -fuse.EINVAL, ""
	case "":
		return -fuse.ENOENT, ""
	default:
		if extractor, ok := fsys.FS.(filesystem.SymlinkFS); ok {
			goPath, err := fuseToGo(path)
			if err != nil {
				fsys.logError(path, err)
				return interpretError(err), ""
			}
			fsLink, err := extractor.ReadLink(goPath)
			if err != nil {
				fsys.logError(path, err)
				return interpretError(err), ""
			}
			fuseLink := posixRoot + fsLink
			return operationSuccess, fuseLink
		}
		return -fuse.ENOSYS, ""
	}
}

func (fsys *fileSystem) Flush(path string, fh fileDescriptor) errNo {
	defer fsys.systemLock.Modify(path)()
	return -fuse.ENOSYS
}
