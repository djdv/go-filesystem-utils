package cgofuse

import (
	"errors"
	"fmt"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
)

type errNo = int

// TODO: rename translate error? transform error?
func interpretError(err error) errNo {
	var fsErr *fserrors.Error
	if errors.As(err, &fsErr) {
		// Translation table for interface.Error -> FUSE error
		return map[fserrors.Kind]errNo{
			fserrors.Other:            -fuselib.EIO,
			fserrors.InvalidItem:      -fuselib.EINVAL,
			fserrors.InvalidOperation: -fuselib.ENOSYS,
			fserrors.Permission:       -fuselib.EACCES,
			fserrors.IO:               -fuselib.EIO,
			fserrors.Exist:            -fuselib.EEXIST,
			fserrors.NotExist:         -fuselib.ENOENT,
			fserrors.IsDir:            -fuselib.EISDIR,
			fserrors.NotDir:           -fuselib.ENOTDIR,
			fserrors.NotEmpty:         -fuselib.ENOTEMPTY,
		}[fsErr.Kind]
	}
	panic(fmt.Sprintf("provided error is not translatable to POSIX error %#v", err))
}
