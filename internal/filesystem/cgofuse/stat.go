//go:build !nofuse

package cgofuse

import (
	"io/fs"

	"github.com/winfsp/cgofuse/fuse"
)

func (gw *goWrapper) Statfs(path string, stat *fuse.Statfs_t) int {
	gw.log.Printf("Statfs %q", path)
	defer gw.systemLock.Access(path)()
	return -fuse.ENOSYS
}

func (gw *goWrapper) Getattr(path string, stat *fuse.Stat_t, fh uint64) int {
	defer gw.systemLock.Access(path)()
	goPath, err := fuseToGo(path)
	if err != nil {
		gw.log.Print(err)
		// TODO: review; should we return the value raw or send err to a converter?
		// ^ send a stacked err to a converter*
		// (so that the trace contains both ops, parent-op+path-lexer+reason)
		// TODO: re-read spec. This is the closest value that seemed appropriate
		// but maybe ACCESS or NOENT makes more sense.
		return -fuse.EINVAL
	}

	if fh != errorHandle {
		// TODO: fh lookup
	}

	// TODO: review
	goStat, err := fs.Stat(gw.FS, goPath)
	if err != nil {
		errNo := interpretError(err)
		// Don't flood the logs with "not found" errors.
		if errNo != -fuse.ENOENT {
			// TODO: [DBG] reduce this format
			gw.log.Printf("path: %s\ngoPath: %s\nerr:%s", path, goPath, err)
		}
		return errNo
	}

	// fsys.log.Printf("stat for %s\n%#v", path, goStat)

	// TODO: don't change stat on the fuse object
	// push changes back to fs.FS via extension
	// fs.SetAttr, fs.SetAttrFuse(path, someOverlappingAttrType)

	mTime := fuse.NewTimespec(goStat.ModTime())

	stat.Uid, stat.Gid, _ = fuse.Getcontext()
	stat.Mode = goToFuseFileType(goStat.Mode()) |
		IRXA // TODO: permissions from root <- options <- cli
		// TODO: mask <- check spec; does fuse need one or does it apply one itself?
		// IRXA&^(fuselib.S_IXOTH)
	stat.Size = goStat.Size()
	// TODO: block size

	// TODO: [devel] `File` needs extensions for these times and we should use them conditionally
	// something like `if aTimer ok; stat.Atim = aTimer.Time()`
	// For now we cheat and use the same value for all
	stat.Atim, // XXX: This shouldn't even be legal syntax.
		stat.Mtim,
		stat.Ctim,
		stat.Birthtim = mTime,
		mTime,
		mTime,
		mTime

	/*
		if path != "/" {
			log.Printf("%s - mode pre conversion: %d, %s",
				path,
				goStat.Mode(), goStat.Mode())
			log.Printf("%s - mode post conversion (masked): %d %d|%d",
				path,
				stat.Mode,
				stat.Mode&fuselib.S_IFMT, stat.Mode&^fuselib.S_IFMT,
			)
		}
	*/

	return operationSuccess
}

func (gw *goWrapper) Utimens(path string, tmsp []fuse.Timespec) int {
	gw.log.Printf("Utimens {%v}%q", tmsp, path)
	defer gw.systemLock.Modify(path)()
	return -fuse.ENOSYS
}

func (gw *goWrapper) Setxattr(path, name string, value []byte, flags int) int {
	gw.log.Printf("Setxattr {%X|%s|%d}%q", flags, name, len(value), path)
	defer gw.systemLock.Modify(path)()
	return -fuse.ENOSYS
}

func (gw *goWrapper) Listxattr(path string, fill func(name string) bool) int {
	gw.log.Printf("Listxattr %q", path)
	defer gw.systemLock.Access(path)()
	return -fuse.ENOSYS
}

func (gw *goWrapper) Getxattr(path, name string) (int, []byte) {
	gw.log.Printf("Getxattr {%s}%q", name, path)
	defer gw.systemLock.Access(path)()
	return -fuse.ENOSYS, nil
}

func (gw *goWrapper) Removexattr(path, name string) int {
	gw.log.Printf("Removexattr {%s}%q", name, path)
	defer gw.systemLock.Modify(path)()
	return -fuse.ENOSYS
}
