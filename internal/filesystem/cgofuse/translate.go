package cgofuse

import (
	"errors"
	"io/fs"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/winfsp/cgofuse/fuse"
)

const (
	goRoot       = "."
	errEmptyPath = generic.ConstError("path argument is empty")
)

// fuseToGo converts a FUSE absolute path
// to a relative [fs.FS] name.
func fuseToGo(path string) (string, error) {
	switch path {
	case "":
		return "", &fserrors.Error{
			PathError: fs.PathError{
				Op:   "fuseToGo",
				Path: path,
				Err:  errEmptyPath,
			},
			Kind: fserrors.InvalidItem,
		}
	case posixRoot:
		return goRoot, nil
	}

	// TODO: does fuse guarantee slash prefixed paths?
	return path[1:], nil
}

func fuseToGoPair(path1, path2 string) (string, string, error) {
	new1, err := fuseToGo(path1)
	if err != nil {
		return "", "", err
	}
	new2, err := fuseToGo(path2)
	if err != nil {
		return "", "", err
	}
	return new1, new2, nil
}

func goToFuseStat(info fs.FileInfo, fctx fuseContext, stat *fuse.Stat_t) {
	var (
		goMode          = info.Mode()
		fuseType        = goToFuseFileType(goMode)
		fusePermissions = goToFusePermissions(goMode)
		fuseModTime     = fuse.NewTimespec(info.ModTime())
	)

	stat.Mode = fuseType | fusePermissions
	stat.Uid = fctx.uid
	stat.Gid = fctx.gid
	stat.Size = info.Size()

	if atimer, ok := info.(filesystem.AccessTimeInfo); ok {
		stat.Atim = fuse.NewTimespec(atimer.AccessTime())
	} else {
		stat.Atim = fuseModTime
	}
	stat.Mtim = fuseModTime
	if ctimer, ok := info.(filesystem.ChangeTimeInfo); ok {
		stat.Ctim = fuse.NewTimespec(ctimer.ChangeTime())
	} else {
		stat.Ctim = fuseModTime
	}
	// TODO: Block size + others.
	if crtimer, ok := info.(filesystem.CreationTimeInfo); ok {
		stat.Birthtim = fuse.NewTimespec(crtimer.CreationTime())
	}
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
var (
	goToFusePermissionsTable = [...]struct {
		golang fs.FileMode
		fuse   filePermissions
	}{
		{golang: filesystem.ExecuteOther, fuse: executeOther},
		{golang: filesystem.WriteOther, fuse: writeOther},
		{golang: filesystem.ReadOther, fuse: readOther},
		{golang: filesystem.ExecuteGroup, fuse: executeGroup},
		{golang: filesystem.WriteGroup, fuse: writeGroup},
		{golang: filesystem.ReadGroup, fuse: readGroup},
		{golang: filesystem.ExecuteUser, fuse: executeUser},
		{golang: filesystem.WriteUser, fuse: writeUser},
		{golang: filesystem.ReadUser, fuse: readUser},
	}
	goFlagsFromFuseTable = [...]struct {
		fuse, golang int
	}{
		{fuse: fuse.O_APPEND, golang: os.O_APPEND},
		{fuse: fuse.O_CREAT, golang: os.O_CREATE},
		{fuse: fuse.O_EXCL, golang: os.O_EXCL},
		{fuse: fuse.O_TRUNC, golang: os.O_TRUNC},
	}
	fsErrorsTable = map[fserrors.Kind]errNo{
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
		fserrors.Recursion:        -fuse.ELOOP,
		fserrors.Closed:           -fuse.EBADF,
	}
)

// TODO: better names
func goToFusePermissions(m fs.FileMode) filePermissions {
	var (
		goPermissions   = m.Perm()
		fusePermissions filePermissions
	)
	for _, bit := range goToFusePermissionsTable {
		if goPermissions&bit.golang != 0 {
			fusePermissions |= bit.fuse
		}
	}
	return fusePermissions
}

func fuseToGoPermissions(m filePermissions) fs.FileMode {
	var fsPermissions fs.FileMode
	for _, bit := range goToFusePermissionsTable {
		if m&bit.fuse != 0 {
			fsPermissions |= bit.golang
		}
	}
	return fsPermissions
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
	for _, bit := range goFlagsFromFuseTable {
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
		return fsErrorsTable[fsErr.Kind]
	}
	return -fuse.EIO
}
