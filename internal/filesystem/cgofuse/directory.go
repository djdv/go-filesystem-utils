package cgofuse

import (
	"io/fs"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/winfsp/cgofuse/fuse"
)

type goDirWrapper struct {
	fs.ReadDirFile
	uid, gid uint32
}

func (gw *goWrapper) Opendir(path string) (int, uint64) {
	defer gw.systemLock.Access(path)()

	openDirFS, ok := gw.FS.(filesystem.OpenDirFS)
	if !ok {
		if idFS, ok := gw.FS.(filesystem.IDer); ok {
			gw.log.Print("Opendir not supported by provided ", idFS.ID()) // TODO: better message
		} else {
			gw.log.Print("Opendir not supported by provided fs.FS") // TODO: better message
		}
		return -fuse.ENOSYS, errorHandle
	}

	goPath, err := fuseToGo(path)
	if err != nil {
		// TODO: review; POSIX spec - make sure errno is appropriate for this op
		gw.log.Print(err)
		return interpretError(err), errorHandle
	}

	directory, err := openDirFS.OpenDir(goPath)
	if err != nil {
		gw.log.Print(err)
		return interpretError(err), errorHandle
	}

	uid, gid, _ := fuse.Getcontext()
	wrappedDir := goDirWrapper{
		ReadDirFile: directory,
		uid:         uid,
		gid:         gid,
	}

	handle, err := gw.fileTable.add(wrappedDir)
	if err != nil {
		gw.log.Print(err)
		return -fuse.EMFILE, errorHandle
	}

	return operationSuccess, handle
}

func (gw *goWrapper) Releasedir(path string, fh uint64) int {
	errNo, err := gw.fileTable.release(fh)
	if err != nil {
		gw.log.Print(err)
	}
	return errNo
}

func (gw *goWrapper) Readdir(path string,
	fill func(name string, stat *fuse.Stat_t, ofst int64) bool,
	ofst int64,
	fh uint64,
) int {
	defer gw.systemLock.Access(path)()

	if fh == errorHandle {
		gw.log.Print(fuse.Error(-fuse.EBADF))
		return -fuse.EBADF
	}

	directory, err := gw.fileTable.get(fh)
	if err != nil {
		gw.log.Print(fuse.Error(-fuse.EBADF))
		return -fuse.EBADF
	}
	dir, ok := directory.goFile.(goDirWrapper)
	if !ok {
		// TODO: error message; unexpected type was stored in file table.
		gw.log.Print(fuse.Error(-fuse.EBADF))
		return -fuse.EBADF
	}

	// TODO: inefficient, store these on open? Use stream?
	ents, err := dir.ReadDir(0)
	if err != nil {
		gw.log.Print(fuse.Error(-fuse.EIO))
		return -fuse.EIO
	}

	for _, ent := range ents {
		entStat, err := dirStat(ent)
		if err != nil {
			gw.log.Print(err)
			return -fuse.EIO
		}
		if entStat != nil {
			if entStat.Uid == posixOmittedID {
				entStat.Uid = dir.uid
			}
			if entStat.Gid == posixOmittedID {
				entStat.Uid = dir.uid
			}
		}
		if !fill(ent.Name(), entStat, 0) {
			break // fill asked us to stop filling.
		}
	}
	return operationSuccess
}
