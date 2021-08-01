//+build !nofuse

package cgofuse

import (
	"context"
	"time"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
)

func (fs *hostBinding) Opendir(path string) (int, uint64) {
	fs.log.Debugf("Opendir - %q", path)

	if path == "" { // invalid requests
		fs.log.Error(fuselib.Error(-fuselib.ENOENT))
		return -fuselib.ENOENT, errorHandle
	}

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

	directory, err := fs.nodeInterface.OpenDirectory(path)
	if err != nil {
		fs.log.Error(err)
		return interpretError(err), errorHandle
	}

	if canReaddirPlus {
		// NOTE: we won't have access to the fuse context in `Readdir` (depending on the fuse implementation)
		// so we associate IDs with the caller who opened the directory
		var ids statIDGroup
		ids.uid, ids.gid, _ = fuselib.Getcontext()
		templateStat := new(fuselib.Stat_t)
		applyCommonsToStat(templateStat, fs.filesWritable, fs.mountTimeGroup, ids)

		directory = upgradeDirectory(directory, fs.readdirplusGen(fs.nodeInterface, path, templateStat))
	}

	handle, err := fs.directories.Add(directory)
	if err != nil { // TODO: transform error
		fs.log.Error(fuselib.Error(-fuselib.EMFILE))
		return -fuselib.EMFILE, errorHandle
	}

	return operationSuccess, handle
}

func (fs *hostBinding) Releasedir(path string, fh uint64) int {
	fs.log.Debugf("Releasedir - {%X}%q", fh, path)

	errNo, err := releaseDir(fs.directories, fh)
	if err != nil {
		fs.log.Error(err)
	}

	return errNo
}

func (fs *hostBinding) Readdir(path string,
	fill func(name string, stat *fuselib.Stat_t, ofst int64) bool,
	ofst int64,
	fh uint64) int {
	fs.log.Debugf("Readdir - {%X|%d}%q", fh, ofst, path)

	if fh == errorHandle {
		fs.log.Error(fuselib.Error(-fuselib.EBADF))
		return -fuselib.EBADF
	}

	directory, err := fs.directories.Get(fh)
	if err != nil {
		fs.log.Error(fuselib.Error(-fuselib.EBADF))
		return -fuselib.EBADF
	}

	// TODO: change this context; needs parent
	callCtx, cancel := context.WithTimeout(context.TODO(), 30*time.Second)
	defer cancel()

	errNo, err := fillDir(callCtx, directory, fill, ofst)
	if err != nil {
		fs.log.Error(err)
	}

	return errNo
}
