package cgofuse

import (
	"io/fs"

	"github.com/u-root/uio/ulog"
	"github.com/winfsp/cgofuse/fuse"
)

const (
	goRoot = "."
)

type goWrapper struct {
	fileTable
	systemLock operationsLock
	fs.FS
	log ulog.Logger
}

func (fs *goWrapper) Init() {
	fs.log.Print("Init")
	fs.fileTable = newFileTable()
	fs.systemLock = newOperationsLock()
	defer fs.log.Print("Init finished")
}

func (fsys *goWrapper) Getattr(path string, stat *fuse.Stat_t, fh uint64) int {
	defer fsys.systemLock.Access(path)()
	fsys.log.Printf("Getattr - {%X}%q", fh, path)

	goPath, err := fuseToGo(path)
	if err != nil {
		fsys.log.Print(err)
		// TODO: review; should we return the value raw or send err to a converter?
		// ^ send a stacked err to a converter*
		// (so that the trace contains both ops, parent-op+path-lexer+reason)
		return 0
	}

	// TODO: fh lookup

	// TODO: review
	goStat, err := fs.Stat(fsys.FS, goPath)
	if err != nil {
		errNo := interpretError(err)
		// Don't flood the logs with "not found" errors.
		if errNo != -fuse.ENOENT {
			fsys.log.Print(err)
		}
		return errNo
	}

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

func (fs *goWrapper) Destroy() {
	// TODO: dbg lint
	fs.log.Print("Destroy")
	defer fs.log.Print("Destroy finished")
	/* TODO: something like this for the new system
	tell the Go FS we're leaving, which itself should have some reference counter.
	we also need to track and close our handles again.
	Old code:
	defer func() {
		if fs.destroySignal != nil {
			// TODO: close all file/dir indices, stream errors out to destroy chan
			close(fs.destroySignal)
			fs.destroySignal = nil
		}
		fs.log.Debugf("Destroy finished")
	}()
	*/

	if err := fs.fileTable.Close(); err != nil {
		fs.log.Print("failed to close:", err)
	}
}
