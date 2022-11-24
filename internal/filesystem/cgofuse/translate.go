//go:build !nofuse

package cgofuse

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/u-root/uio/ulog"
	"github.com/winfsp/cgofuse/fuse"
)

// TODO: better name
func GoToFuse(fs fs.FS) (*Fuse, error) {
	fsh := fuse.NewFileSystemHost(&goWrapper{
		FS: fs,
		// fileTable:  newFileTable(),
		// systemLock: newOperationsLock(),
		// log:        ulog.Null, // TODO: from options
		log: ulog.Log,
	})
	// TODO: from options.
	canReaddirPlus := runtime.GOOS == "windows"
	fsh.SetCapReaddirPlus(canReaddirPlus)
	fsh.SetCapCaseInsensitive(false)
	//
	return &Fuse{FileSystemHost: fsh}, nil
	// TODO: WithLog(...) option.
	// var eLog logging.EventLogger
	// if idFs, ok := fs.(filesystem.IdentifiedFS); ok {
	// 	eLog = log.New(idFs.ID().String())
	// } else {
	// 	eLog = log.New("ipfs-core")
	// }

	// sysLog := ulog.Null
	// const logStub = false // TODO: from CLI flags / funcopts.
	// if logStub {
	// 	// sysLog = log.Default()
	// 	sysLog = log.New(os.Stdout, "fuse dbg - ", log.Lshortfile)
	// }
	// return &hostBinding{
	// 	goFs: fs,
	// 	log:  sysLog,
	// }
}

// fuseToGo converts a FUSE absolute path
// to a relative [fs.FS] name.
func fuseToGo(path string) (string, error) {
	const op fserrors.Op = "path lexer"
	switch path {
	case "":
		return "", fserrors.New(op,
			fserrors.Path("{empty-string}"),
			fserrors.InvalidItem,
		)
	case posixRoot:
		return goRoot, nil
	}

	// TODO: does fuse guarantee slash prefixed paths?
	return path[1:], nil
}

// [FileMode] to FUSE mode bits.
func goToFuseFileType(m fs.FileMode) fileType {
	switch m.Type() {
	case fs.ModeDir:
		return fuse.S_IFDIR
	case fs.FileMode(0):
		return fuse.S_IFREG
	case fs.ModeSymlink:
		return fuse.S_IFLNK
	default:
		return 0
	}
}

// TODO: better names
func goFlagsFromFuse(fuseFlags int) int {
	var goFlags int
	switch fuseFlags & fuse.O_ACCMODE {
	case fuse.O_RDONLY:
		goFlags = os.O_RDONLY
	case fuse.O_WRONLY:
		goFlags = os.O_WRONLY
	case fuse.O_RDWR:
		goFlags = os.O_RDWR
	}
	for _, bit := range []struct {
		fuse, golang int
	}{
		{fuse: fuse.O_APPEND, golang: os.O_APPEND},
		{fuse: fuse.O_CREAT, golang: os.O_CREATE},
		{fuse: fuse.O_EXCL, golang: os.O_EXCL},
		{fuse: fuse.O_TRUNC, golang: os.O_TRUNC},
	} {
		if fuseFlags&bit.fuse != 0 {
			goFlags |= bit.golang
		}
	}
	return goFlags
}

// TODO: rename translate error? transform error?
func interpretError(err error) errNo {
	var fsErr *fserrors.Error
	if errors.As(err, &fsErr) {
		// Translation table for interface.Error -> FUSE error
		return map[fserrors.Kind]errNo{
			fserrors.Other:            -fuse.EIO,
			fserrors.InvalidItem:      -fuse.EINVAL,
			fserrors.InvalidOperation: -fuse.ENOSYS,
			fserrors.Permission:       -fuse.EACCES,
			fserrors.IO:               -fuse.EIO,
			fserrors.Exist:            -fuse.EEXIST,
			fserrors.NotExist:         -fuse.ENOENT,
			fserrors.IsDir:            -fuse.EISDIR,
			fserrors.NotDir:           -fuse.ENOTDIR,
			fserrors.NotEmpty:         -fuse.ENOTEMPTY,
		}[fsErr.Kind]
	}
	panic(fmt.Sprintf("provided error is not translatable to POSIX error %#v", err))
}
