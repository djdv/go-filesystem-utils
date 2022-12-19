//go:build !nofuse

package cgofuse

import (
	"io/fs"

	"github.com/winfsp/cgofuse/fuse"
)

func (gw *goWrapper) Statfs(path string, stat *fuse.Statfs_t) errNo {
	defer gw.systemLock.Access(path)()
	return -fuse.ENOSYS
}

func (gw *goWrapper) Getattr(path string, stat *fuse.Stat_t, fh fileDescriptor) errNo {
	defer gw.systemLock.Access(path)()
	var (
		info fs.FileInfo
		err  error
	)
	if fh != errorHandle {
		info, err = gw.infoFromHandle(fh)
	} else {
		info, err = gw.infoFromPath(path)
	}
	if err != nil {
		errNo := interpretError(err)
		if errNo != -fuse.ENOENT {
			// Don't flood the logs with these.
			gw.logError(path, err)
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

func (gw *goWrapper) infoFromHandle(fh fileDescriptor) (fs.FileInfo, error) {
	file, err := gw.fileTable.get(fh)
	if err != nil {
		return nil, err
	}
	return file.goFile.Stat()
}

func (gw *goWrapper) infoFromPath(path string) (fs.FileInfo, error) {
	goPath, err := fuseToGo(path)
	if err != nil {
		return nil, err
	}
	return fs.Stat(gw.FS, goPath)
}

func (gw *goWrapper) Utimens(path string, tmsp []fuse.Timespec) errNo {
	defer gw.systemLock.Modify(path)()
	return -fuse.ENOSYS
}

func (gw *goWrapper) Setxattr(path, name string, value []byte, flags int) errNo {
	defer gw.systemLock.Modify(path)()
	return -fuse.ENOSYS
}

func (gw *goWrapper) Listxattr(path string, fill func(name string) bool) errNo {
	defer gw.systemLock.Access(path)()
	return -fuse.ENOSYS
}

func (gw *goWrapper) Getxattr(path, name string) (errNo, []byte) {
	defer gw.systemLock.Access(path)()
	return -fuse.ENOSYS, nil
}

func (gw *goWrapper) Removexattr(path, name string) errNo {
	defer gw.systemLock.Modify(path)()
	return -fuse.ENOSYS
}
