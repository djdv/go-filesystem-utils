package cgofuse

import (
	"errors"
	"io/fs"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/winfsp/cgofuse/fuse"
)

func (fsys *fileSystem) Statfs(path string, stat *fuse.Statfs_t) errNo {
	defer fsys.systemLock.Access(path)()
	return -fuse.ENOSYS
}

func (fsys *fileSystem) Getattr(path string, stat *fuse.Stat_t, fh fileDescriptor) errNo {
	defer fsys.systemLock.Access(path)()
	if path == fsys.mountID {
		// Special case; see: [Mount].
		stat.Mode = 0o111 | fuse.S_IFREG
		return operationSuccess
	}
	var (
		info fs.FileInfo
		err  error
	)
	if fh != errorHandle {
		info, err = fsys.infoFromHandle(fh)
	} else {
		info, err = fsys.infoFromPath(path)
	}
	if err != nil {
		errNo := interpretError(err)
		if errNo != -fuse.ENOENT {
			// Don't flood the logs with these.
			fsys.logError(path, err)
		}
		return errNo
	}
	var (
		uid, gid, _ = fuse.Getcontext()
		fctx        = fuseContext{uid: uid, gid: gid}
	)
	goToFuseStat(info, fctx, stat)
	return operationSuccess
}

func (fsys *fileSystem) infoFromHandle(fh fileDescriptor) (fs.FileInfo, error) {
	file, err := fsys.fileTable.get(fh)
	if err != nil {
		return nil, err
	}
	return file.goFile.Stat()
}

func (fsys *fileSystem) infoFromPath(path string) (fs.FileInfo, error) {
	goPath, err := fuseToGo(path)
	if err != nil {
		return nil, err
	}
	if stat, err := filesystem.Lstat(fsys.FS, goPath); err == nil {
		return stat, nil
	} else if !errors.Is(err, errors.ErrUnsupported) {
		return nil, err
	}
	return fs.Stat(fsys.FS, goPath)
}

func (fsys *fileSystem) access(path string, mask uint32) errNo {
	if mask&^(fuse.F_OK|
		fuse.R_OK|
		fuse.W_OK|
		fuse.X_OK) != 0 {
		return -fuse.EINVAL
	}
	info, err := fsys.infoFromPath(path)
	if err != nil {
		errNo := interpretError(err)
		if errNo != -fuse.ENOENT {
			// Don't flood the logs with these.
			fsys.logError(path, err)
		}
		return errNo
	}
	// TODO: if the [fs.FileInfo] is extended
	// to contain UID and GID values, use them.
	// For now, we disregard ownership security.
	// The process owner that called us,
	// owns the file during this check.
	var (
		cUID, cGID, _ = fuse.Getcontext()
		fUID, fGID    = cUID, cGID
		userPerms     = cUID == fUID
		groupPerms    = cGID == fGID
		permissions   = goToFusePermissions(info.Mode())
		failed        bool
		check         = func(otherBits uint32, userBits, groupBits bool) {
			checkMask := otherBits
			if userBits {
				const userBitsOffset = 6
				checkMask |= (otherBits << userBitsOffset)
			}
			if groupBits {
				const groupBitsOffset = 3
				checkMask |= (otherBits << groupBitsOffset)
			}
			if noAuth := permissions&checkMask == 0; noAuth {
				failed = true
			}
		}
	)
	if checkRead := mask&fuse.R_OK != 0; checkRead {
		check(fuse.S_IROTH, userPerms, groupPerms)
	}
	if checkWrite := mask&fuse.W_OK != 0; checkWrite {
		check(fuse.S_IWOTH, userPerms, groupPerms)
	}
	if checkExec := mask&fuse.X_OK != 0; checkExec {
		check(fuse.S_IXOTH, userPerms, groupPerms)
	}
	if failed {
		return -fuse.EACCES
	}
	return operationSuccess
}

func (fsys *fileSystem) Chmod(path string, mode uint32) errNo {
	defer fsys.systemLock.Modify(path)()
	return -fuse.ENOSYS
}

func (fsys *fileSystem) Chown(path string, uid, gid uint32) errNo {
	defer fsys.systemLock.Modify(path)()
	return -fuse.ENOSYS
}

func (fsys *fileSystem) Utimens(path string, tmsp []fuse.Timespec) errNo {
	defer fsys.systemLock.Modify(path)()
	return -fuse.ENOSYS
}

func (fsys *fileSystem) Setxattr(path, name string, value []byte, flags int) errNo {
	defer fsys.systemLock.Modify(path)()
	return -fuse.ENOSYS
}

func (fsys *fileSystem) Listxattr(path string, fill func(name string) bool) errNo {
	defer fsys.systemLock.Access(path)()
	return -fuse.ENOSYS
}

func (fsys *fileSystem) Getxattr(path, name string) (errNo, []byte) {
	defer fsys.systemLock.Access(path)()
	return -fuse.ENOSYS, nil
}

func (fsys *fileSystem) Removexattr(path, name string) errNo {
	defer fsys.systemLock.Modify(path)()
	return -fuse.ENOSYS
}
