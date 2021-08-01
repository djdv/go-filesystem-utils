//+build !nofuse

package cgofuse

import (
	"errors"
	"fmt"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
	fserrors "github.com/ipfs/go-ipfs/filesystem/errors"
)

func interpretError(err error) errNo {
	if present := errors.Unwrap(err); present != nil {
		err = present
	}

	if errIntf, ok := err.(fserrors.Error); ok {
		return map[fserrors.Kind]errNo{ // translation table for interface.Error -> FUSE error
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
		}[errIntf.Kind()]
	}

	panic(fmt.Sprintf("provided error is not translatable to POSIX error %#v", err))
}
