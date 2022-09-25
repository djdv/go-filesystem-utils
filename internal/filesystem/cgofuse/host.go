package cgofuse

import (
	"io/fs"
	"log"
	"math"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/u-root/uio/ulog"
)

const (
	operationSuccess = 0
	errorHandle      = math.MaxUint64
	handleMax        = errorHandle - 1

	S_IRWXO = fuselib.S_IROTH | fuselib.S_IWOTH | fuselib.S_IXOTH
	S_IRWXG = fuselib.S_IRGRP | fuselib.S_IWGRP | fuselib.S_IXGRP
	S_IRWXU = fuselib.S_IRUSR | fuselib.S_IWUSR | fuselib.S_IXUSR

	IRWXA = S_IRWXU | S_IRWXG | S_IRWXO                                    // 0o777
	IRXA  = IRWXA &^ (fuselib.S_IWUSR | fuselib.S_IWGRP | fuselib.S_IWOTH) // 0o555
)

type goWrapper struct {
	fileTable
	systemLock operationsLock
	fs.FS
	log ulog.Logger // general operations log
}

const (
	posixRoot = "/"
	goRoot    = "."
)

// FUSE absolute path to relative [fs.FS] name.
func fuseToGo(path string) (name string, _ error) {
	const op errors.Op = "path lexer"
	switch path {
	case "":
		return "", errors.New(op,
			errors.Path("{empty-string}"),
			errors.InvalidItem,
		)
	case posixRoot:
		return goRoot, nil
	}

	// TODO: does fuse guarantee slash prefixed paths?
	return path[1:], nil
}

type fuseFileType = uint32

// [FileMode] to FUSE mode bits.
func goToFuseFileType(m fs.FileMode) fuseFileType {
	switch m.Type() {
	case fs.ModeDir:
		return fuselib.S_IFDIR
	case fs.FileMode(0):
		return fuselib.S_IFREG
	case fs.ModeSymlink:
		return fuselib.S_IFLNK
	default:
		return 0
	}
}

func (fs *goWrapper) Init() {
	fs.log.Print("Init")
	fs.fileTable = newFileTable()
	fs.systemLock = newOperationsLock()
	defer fs.log.Print("Init finished")
}

func (fuse *goWrapper) Getattr(path string, stat *fuselib.Stat_t, fh uint64) int {
	defer fuse.systemLock.Access(path)()
	fuse.log.Printf("Getattr - {%X}%q", fh, path)

	defer func() {
		if r := recover(); r != nil {
			log.Printf("🔥 HOT TONIGHT - %s 🔥 %v", path, r)
		}
	}()

	goPath, err := fuseToGo(path)
	if err != nil {
		fuse.log.Print(err)
		// TODO: review; should we return the value raw or send err to a converter?
		// ^ send a stacked err to a converter*
		// (so that the trace contains both ops, parent-op+path-lexer+reason)
		return 0
	}

	// TODO: fh lookup

	// TODO: review
	goStat, err := fs.Stat(fuse.FS, goPath)
	if err != nil {
		errNo := interpretError(err)
		// Don't flood the logs with "not found" errors.
		if errNo != -fuselib.ENOENT {
			fuse.log.Print(err)
		}
		return errNo
	}

	// TODO: don't change stat on the fuse object
	// push changes back to fs.FS via extension
	// fs.SetAttr, fs.SetAttrFuse(path, someOverlappingAttrType)

	mTime := fuselib.NewTimespec(goStat.ModTime())

	stat.Uid, stat.Gid, _ = fuselib.Getcontext()
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
