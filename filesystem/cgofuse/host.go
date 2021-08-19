package cgofuse

import (
	"io/fs"
	"math"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/filesystem/errors"
	logging "github.com/ipfs/go-log"
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

type hostBinding struct {
	//fuselib.FileSystemBase // TODO: remove this
	fileTable
	goFs fs.FS
	log  logging.EventLogger // general operations log
}

func NewFuseInterface(fs fs.FS) (fuselib.FileSystemInterface, error) {
	// TODO: WithLog(...) option.
	var log logging.EventLogger
	if idFs, ok := fs.(filesystem.IdentifiedFS); ok {
		log = logging.Logger(idFs.ID().String())
	} else {
		log = logging.Logger("ipfs-core")
	}

	// TODO: [port] we need to mimic `ipfs log` elswhere
	//logging.SetAllLoggers(logging.LevelDebug)
	logging.SetAllLoggers(logging.LevelError)

	return &hostBinding{
		goFs: fs,
		log:  log,
	}, nil
}

const (
	posixRoot = "/"
	goRoot    = "."
)

// My new restaurant.
func posixToGo(path string) (name string, _ error) {
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

// Do not pass Go, do not collect 200 Unix bits.
func goToPosix(m fs.FileMode) fuseFileType {
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

func (fs *hostBinding) Init() {
	fs.log.Debugf("Init")
	fs.fileTable = newFileTable()
	defer fs.log.Debugf("Init finished")
}

func (fuse *hostBinding) Getattr(path string, stat *fuselib.Stat_t, fh uint64) int {
	fuse.log.Debugf("Getattr - {%X}%q", fh, path)

	goPath, err := posixToGo(path)
	if err != nil {
		fuse.log.Error(err)
		// TODO: review; should we return the value raw or send err to a converter?
		// ^ send a stacked err to a converter*
		// (so that the trace contains both ops, parent-op+path-lexer+reason)
		return 0
	}

	// TODO: fh lookup

	// TODO: review
	goStat, err := fs.Stat(fuse.goFs, goPath)
	if err != nil {
		errNo := interpretError(err)
		// Don't flood the logs with "not found" errors.
		if errNo != -fuselib.ENOENT {
			fuse.log.Error(err)
		}
		return errNo
	}
	mTime := fuselib.NewTimespec(goStat.ModTime())

	stat.Uid, stat.Gid, _ = fuselib.Getcontext()
	stat.Mode = goToPosix(goStat.Mode()) |
		IRXA&^(fuselib.S_IXOTH)
	stat.Size = goStat.Size()
	// TODO: block size

	// TODO: [devel] `File` needs extensions for these times and we should use them conditionally
	// something like `if aTimer ok; stat.Atim = aTimer.Time()`
	// For now we cheat and use the same value for all
	stat.Atim, // XXX: This shouldn't even be legal syntax.
		stat.Mtim,
		stat.Ctim,
		stat.Birthtim =
		mTime,
		mTime,
		mTime,
		mTime

	return operationSuccess
}

func (fs *hostBinding) Destroy() {
	fs.log.Debugf("Destroy")
	defer fs.log.Debugf("Destroy finished")
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
		fs.log.Error("failed to close:", err)
	}
}
