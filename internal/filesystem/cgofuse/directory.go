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
	gw.log.Printf("Opendir - %q", path)

	openDirFS, ok := gw.FS.(filesystem.OpenDirFS)
	if !ok {
		// TODO: proper interface IDFS?
		if idFS, ok := gw.FS.(interface {
			ID() filesystem.ID
		}); ok {
			gw.log.Print("Opendir not supported by provided ", idFS.ID()) // TODO: better message
		} else {
			gw.log.Print("Opendir not supported by provided fs.FS") // TODO: better message
		}
		// NOTE: Most fuse implementations consider this to be final.
		// And will never try this operation again.
		return -fuse.ENOSYS, errorHandle
	}

	goPath, err := fuseToGo(path)
	if err != nil {
		// TODO: review; POSIX spec - make sure errno is appropriate for this op
		gw.log.Print(err)
		return interpretError(err), errorHandle
	}

	// Comment below is outdated. We should see if it's possible to to interface with dotnet
	// as a FS provider. Like `env:` is in `pwsh`.
	// FIXME: (add common option like fakeRootEntries or something)
	// on Windows, (specifically in Powerhshell) when mounted to a UNC path
	// operations like `Get-ChildItem "\\servername\share\Qm..."` work fine, but
	// `Set-Location "\\servername\share\Qm..."` always fail
	// this seems to do with the fact the share's root does not actually contain the target
	// (pwsh seems to read the root to verify existence before attempting to changing into it)
	// the same behaviour is not present when mounted to a drivespec like `I:`
	// or in other applications (namely Explorer)
	// We could probably fix this by caching the first component of the last getattr call
	// and `fill`ing it in during Readdir("/")
	// failing this, a more persistent LRU cache could be shown in the root

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

	handle, err := gw.fileTable.Add(wrappedDir)
	if err != nil { // TODO: transform error
		gw.log.Print(fuse.Error(-fuse.EMFILE))
		return -fuse.EMFILE, errorHandle
	}

	return operationSuccess, handle
}

func (gw *goWrapper) Releasedir(path string, fh uint64) int {
	gw.log.Printf("Releasedir - {%X}%q", fh, path)

	errNo, err := releaseFile(gw.fileTable, fh)
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
	gw.log.Printf("Readdir - {%X|%d}%q", fh, ofst, path)

	if fh == errorHandle {
		gw.log.Print(fuse.Error(-fuse.EBADF))
		return -fuse.EBADF
	}

	directory, err := gw.fileTable.Get(fh)
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
		if !fill(ent.Name(), dirStat(ent), 0) {
			break // fill asked us to stop filling.
		}
	}
	return operationSuccess
}
