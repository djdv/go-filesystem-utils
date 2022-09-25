package cgofuse

import (
	"io/fs"
	"log"

	"github.com/billziss-gh/cgofuse/fuse"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
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

/*
func (fuse *hostBinding) Readdir(path string,
	fill func(name string, stat *fuselib.Stat_t, ofst int64) bool,
	ofst int64,
	fh uint64,
) int {
	defer fuse.systemLock.Access(path)()
	fuse.log.Printf("Readdir - {%X|%d}%q", fh, ofst, path)

	if fh == errorHandle {
		fuse.log.Print(fuselib.Error(-fuselib.EBADF))
		return -fuselib.EBADF
	}

	directory, err := fuse.fileTable.Get(fh)
	if err != nil {
		fuse.log.Print(fuselib.Error(-fuselib.EBADF))
		return -fuselib.EBADF
	}

	const buffSize = 4 // TODO: figure out a good one
	var (
		// TODO: We should inherit a context from the host binding
		// guarantee that we die on destroy at least.
		readCtx, readCancel = context.WithCancel(context.TODO())
		entries             = make(chan fs.DirEntry, buffSize)
		// TODO: type safety; revert this when the indexes are split
		errs = filesystem.StreamDir(directory.goFile.(fs.ReadDirFile), readCtx, entries)
	)
	defer readCancel()

	statFn := func(ent fs.DirEntry) *fuselib.Stat_t {
		if !canReaddirPlus {
			return nil
		}

		goStat, err := ent.Info()
		if err != nil {
			fuse.log.Print(err)
			return nil
		}
		mTime := fuselib.NewTimespec(goStat.ModTime())

		// FIXME: We need to obtain these values during Opendir.
		// And assign them here.
		// They are NOT guaranteed to be valid within calls to Readdir.
		// (some platforms may return data here)
		// stat.Uid, stat.Gid, _ = fuselib.Getcontext()
		return &fuselib.Stat_t{
			Mode: goToFuseFileType(goStat.Mode()) |
				IRXA&^(fuselib.S_IXOTH), // TODO: const permissions; used here and in getattr
			Size:     goStat.Size(),
			Atim:     mTime,
			Mtim:     mTime,
			Ctim:     mTime,
			Birthtim: mTime,
		}
	}

	// TODO: The flow control here isn't as obvious or direct as it probably could be.
	for entries != nil {
		select {
		case ent, ok := <-entries:
			if !ok {
				entries = nil
				break
			}
			// TODO: fill in offset if we have persistent|consistent ordering.
			// Right now we delegate offset responsibility to libfuse.
			if !fill(ent.Name(), statFn(ent), 0) {
				// fill asked us to stop filling
				readCancel()
			}
		case err := <-errs:
			fuse.log.Print(err)
			return -fuselib.EIO // TODO: check spec

		case <-readCtx.Done():
			entries = nil
		}
	}

	return operationSuccess
}
*/

func (gw *goWrapper) Readdir(path string,
	fill func(name string, stat *fuse.Stat_t, ofst int64) bool,
	ofst int64,
	fh uint64,
) int {
	defer gw.systemLock.Access(path)()
	gw.log.Printf("Readdir - {%X|%d}%q", fh, ofst, path)

	if fh == errorHandle {
		log.Println("fuse readdir hit 1")
		gw.log.Print(fuse.Error(-fuse.EBADF))
		return -fuse.EBADF
	}

	directory, err := gw.fileTable.Get(fh)
	if err != nil {
		log.Println("fuse readdir hit 2")
		gw.log.Print(fuse.Error(-fuse.EBADF))
		return -fuse.EBADF
	}
	dir, ok := directory.goFile.(goDirWrapper)
	if !ok {
		log.Println("fuse readdir hit 3")
		// TODO: error message; unexpected type was stored in file table.
		gw.log.Print(fuse.Error(-fuse.EBADF))
		return -fuse.EBADF
	}

	// TODO: inefficient, store these on open? Use stream?
	ents, err := dir.ReadDir(0)
	if err != nil {
		log.Println("fuse readdir hit 4")
		gw.log.Print(fuse.Error(-fuse.EIO))
		return -fuse.EIO
	}

	statFn := func(ent fs.DirEntry) *fuse.Stat_t {
		if !canReaddirPlus {
			return nil
		}
		goStat, err := ent.Info()
		if err != nil {
			gw.log.Print(err)
			return nil
		}
		mTime := fuse.NewTimespec(goStat.ModTime())

		// FIXME: We need to obtain these values during Opendir.
		// And assign them here.
		// They are NOT guaranteed to be valid within calls to Readdir.
		// (some platforms may return data here)
		// stat.Uid, stat.Gid, _ = fuselib.Getcontext()
		return &fuse.Stat_t{
			Mode: goToFuseFileType(goStat.Mode()) |
				IRXA&^(fuse.S_IXOTH), // TODO: const permissions; used here and in getattr
			Uid:      dir.uid,
			Gid:      dir.gid,
			Size:     goStat.Size(),
			Atim:     mTime,
			Mtim:     mTime,
			Ctim:     mTime,
			Birthtim: mTime,
		}
	}
	for _, ent := range ents {
		if !fill(ent.Name(), statFn(ent), 0) {
			// fill asked us to stop filling
			break
		}
	}
	return operationSuccess
}
