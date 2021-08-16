package cgofuse

import (
	"io/fs"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
	"github.com/djdv/go-filesystem-utils/filesystem"
)

func (fs *hostBinding) Opendir(path string) (int, uint64) {
	fs.log.Debugf("Opendir - %q", path)

	openDirFS, ok := fs.goFs.(filesystem.OpenDirFS)
	if !ok {
		// NOTE: Most fuse implementations consider this to be final.
		// And will never try this operation again.
		fs.log.Warn("Opendir not supported by provided fs.FS") // TODO: better message
		return -fuselib.ENOSYS, errorHandle
	}

	goPath, err := posixToGo(path)
	if err != nil {
		// TODO: review; POSIX spec - make sure errno is appropriate for this op
		fs.log.Error(err)
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
		fs.log.Error(err)
		return interpretError(err), errorHandle
	}

	/* TODO: [port]
	if canReaddirPlus {
		// NOTE: we won't have access to the fuse context in `Readdir` (depending on the fuse implementation)
		// so we associate IDs with the caller who opened the directory
		var ids statIDGroup
		ids.uid, ids.gid, _ = fuselib.Getcontext()
		templateStat := new(fuselib.Stat_t)
		applyCommonsToStat(templateStat, fs.filesWritable, fs.mountTimeGroup, ids)

		directory = upgradeDirectory(directory, fs.readdirplusGen(fs.nodeInterface, path, templateStat))
	}
	*/

	handle, err := fs.fileTable.Add(directory)
	if err != nil { // TODO: transform error
		fs.log.Error(fuselib.Error(-fuselib.EMFILE))
		return -fuselib.EMFILE, errorHandle
	}

	return operationSuccess, handle
}

func (fs *hostBinding) Releasedir(path string, fh uint64) int {
	fs.log.Debugf("Releasedir - {%X}%q", fh, path)

	errNo, err := releaseFile(fs.fileTable, fh)
	if err != nil {
		fs.log.Error(err)
	}

	return errNo
}

func (fuse *hostBinding) Readdir(path string,
	fill func(name string, stat *fuselib.Stat_t, ofst int64) bool,
	ofst int64,
	fh uint64) int {
	fuse.log.Debugf("Readdir - {%X|%d}%q", fh, ofst, path)

	if fh == errorHandle {
		fuse.log.Error(fuselib.Error(-fuselib.EBADF))
		return -fuselib.EBADF
	}

	directory, err := fuse.fileTable.Get(fh)
	if err != nil {
		fuse.log.Error(fuselib.Error(-fuselib.EBADF))
		return -fuselib.EBADF
	}

	/* TODO: [port] - for now no seeking (one shot; and Fuselib controls cache)
	errNo, err := fillDir(callCtx, directory, fill, ofst)
	if err != nil {
		fs.log.Error(err)
	}
	*/

	// TODO: Consider how best to handle this assertion + error.
	// Right now we let it panic if the fs implimentation is bad
	// We should check this up front on open, and maybe separate the file tables again.
	//entries, err := directory.(fs.ReadDirFile).ReadDir(0)
	dbgDir, ok := directory.(fs.ReadDirFile)
	if !ok {
		fuse.log.Error("⚠️ bad dir, we're about to panic ⚠️")
	} else {
		fuse.log.Warn("good dir")
	}
	entries, err := dbgDir.ReadDir(0)
	if err != nil {
		fuse.log.Error(err)
		return -fuselib.EIO // TODO: check POSIX spec; errno value
	}

	for _, ent := range entries {
		var stat *fuselib.Stat_t
		if canReaddirPlus {
			goStat, err := ent.Info()
			if err != nil {
				fuse.log.Error(err)
				return -fuselib.EIO // TODO: check POSIX spec; errno value
			}
			mTime := fuselib.NewTimespec(goStat.ModTime())
			stat = new(fuselib.Stat_t)

			// FIXME: These values must be obtained and stored during Opendir.
			// They are NOT guaranteed to be valid within calls to Readdir.
			// (some platforms may return data here)
			//stat.Uid, stat.Gid, _ = fuselib.Getcontext()

			stat.Mode = goToPosix(goStat.Mode()) |
				IRXA&^(fuselib.S_IXOTH) // TODO: const permissions; used here and in getattr
			stat.Size = goStat.Size()
			stat.Atim, // XXX: This shouldn't even be legal syntax.
				stat.Mtim, // TODO: dedupe between this and getattr
				stat.Ctim,
				stat.Birthtim =
				mTime,
				mTime,
				mTime,
				mTime
		}
		// TODO: fill in offset if we have persistent|consistent ordering.
		if !fill(ent.Name(), stat, 0) {
			break // fill asked us to stop filling
		}
	}

	return operationSuccess
}
