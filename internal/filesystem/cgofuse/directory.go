//go:build !nofuse

package cgofuse

import (
	"context"
	"io/fs"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/winfsp/cgofuse/fuse"
)

type (
	directoryStream struct {
		fs.ReadDirFile
		// TODO: it might be better to use the fs.DirEntry type for this whole chain
		// and only check the type where these are used.
		// E.g. expect standard directory entries, but check if they're extended
		// with an Error method within readdir.
		// This would eliminate the need for us to wrap the types, higher in the chain.
		// And not depend on extensions as much.
		// ^ The downside of this is that callers might not check for this,
		// and thus miss an error value. So it may not be better.
		// Needs consideration.
		entries <-chan filesystem.StreamDirEntry
		context.Context
		context.CancelFunc
		fuseContext
		position int64
	}
)

const (
	errNotReadDirFile     = generic.ConstError("file does not implement ReadDirFile")
	errDirStreamNotOpened = generic.ConstError("directory stream not opened")
)

func (gw *goWrapper) Mkdir(path string, mode uint32) errNo {
	defer gw.systemLock.CreateOrDelete(path)()
	if maker, ok := gw.FS.(filesystem.MkdirFS); ok {
		goPath, err := fuseToGo(path)
		if err != nil {
			gw.logError(path, err)
			return interpretError(err)
		}
		permissions := fuseToGoPermissions(mode)
		if err := maker.Mkdir(goPath, permissions); err != nil {
			return interpretError(err)
		}
		return operationSuccess
	}
	return -fuse.ENOSYS
}

func (gw *goWrapper) Opendir(path string) (errNo, fileDescriptor) {
	defer gw.systemLock.Access(path)()
	directory, err := openDir(gw.FS, path)
	if err != nil {
		gw.logError(path, err)
		return interpretError(err), errorHandle
	}
	var (
		// NOTE: `fuse_get_context` is only required to
		// return valid data within certain operations.
		// `openddir` is one such operation.
		// Some implementations may return valid data within `readdir`
		// but this can not be relied on.
		// As such, we store it with the stream here, for re-use there.
		// TODO: We should accept these values as options
		// and only use the fuse context if not provided.
		// I.e. caller of the wrapper constructor can define what UID+GID we should use.
		uid, gid, _ = fuse.Getcontext()
		dirStream   = newStreamDir(directory, fuseContext{
			uid: uid,
			gid: gid,
		})
	)
	handle, err := gw.fileTable.add(dirStream)
	if err != nil {
		gw.logError(path, err)
		// TODO: the file table should return an error value
		// that maps to this POSIX error.
		return -fuse.EMFILE, errorHandle
	}
	return operationSuccess, handle
}

func openDir(fsys fs.FS, path string) (fs.ReadDirFile, error) {
	goPath, err := fuseToGo(path)
	if err != nil {
		return nil, err
	}
	file, err := fsys.Open(goPath)
	if err != nil {
		return nil, err
	}
	directory, ok := file.(fs.ReadDirFile)
	if !ok {
		return nil, &fserrors.Error{
			PathError: fs.PathError{
				Op:   "open",
				Path: path,
				Err:  errNotReadDirFile,
			},
			Kind: fserrors.NotDir,
		}
	}
	return directory, nil
}

func (gw *goWrapper) Readdir(path string, fill fillFunc, ofst int64, fh fileDescriptor) errNo {
	defer gw.systemLock.Access(path)()
	if fh == errorHandle {
		const errNo = -fuse.EBADF
		gw.logError(path, fuse.Error(errNo))
		return errNo
	}
	directoryHandle, err := gw.fileTable.get(fh)
	if err != nil {
		gw.logError(path, err)
		return -fuse.EBADF
	}
	var (
		directory  = directoryHandle.goFile
		stream, ok = directory.(*directoryStream)
	)
	if !ok {
		const errNo = -fuse.EBADF
		gw.logError(path, fuse.Error(errNo))
		return errNo
	}
	if ofst == 0 && stream.position != 0 {
		if errorCode, err := rewinddir(gw.FS, stream, path); err != nil {
			gw.logError(path, err)
			return errorCode
		}
	}
	ret, err := fillDir(stream, fill)
	if err != nil {
		gw.logError(path, err)
	}
	return ret
}

func newStreamDir(directory fs.ReadDirFile, fCtx fuseContext) *directoryStream {
	ctx, cancel := context.WithCancel(context.Background())
	const count = 16 // Arbitrary buffer size.
	return &directoryStream{
		Context:     ctx,
		CancelFunc:  cancel,
		ReadDirFile: directory,
		entries:     filesystem.StreamDir(ctx, count, directory),
		fuseContext: fCtx,
	}
}

// NOTE: See SUSv4;BSi7 `rewinddir`.
// FUSE should translate those into (FUSE) `readdir` with offset 0
// ^ TODO: (Re)validate that this is true. Include it in CGO tests as well.
// ^^ With funny business. Directory contents should change between calls.
// opendidr; readdir; modify dir contents; rewinddir; readdir; closedir
func rewinddir(fsys fs.FS, stream *directoryStream, path string) (errNo, error) {
	if !stream.opened() {
		return -fuse.EIO, errDirStreamNotOpened
	}
	stream.CancelFunc()
	directory, err := openDir(fsys, path)
	if err != nil {
		return interpretError(err), err
	}
	*stream = *newStreamDir(directory, stream.fuseContext)
	return operationSuccess, nil
}

func fillDir(stream *directoryStream, fill fillFunc) (errNo, error) {
	var (
		ctx     = stream.Context
		entries = stream.entries
		offset  = stream.position
		fCtx    = stream.fuseContext
	)
	defer func() { stream.position = offset }()
	for {
		select {
		case <-ctx.Done():
			return -fuse.EBADF, ctx.Err()
		case entry, ok := <-entries:
			if !ok {
				return operationSuccess, nil
			}
			offset++
			if err := entry.Error(); err != nil {
				return -fuse.ENOENT, err
			}
			entStat, err := dirStat(entry, fCtx)
			if err != nil {
				return -fuse.EIO, err
			}
			if !fill(entry.Name(), entStat, offset) {
				return operationSuccess, nil
			}
		}
	}
}

func (gw *goWrapper) Fsyncdir(path string, datasync bool, fh fileDescriptor) errNo {
	defer gw.systemLock.Modify(path)()
	return -fuse.ENOSYS
}

func (gw *goWrapper) Releasedir(path string, fh fileDescriptor) errNo {
	errNo, err := gw.fileTable.release(fh)
	if err != nil {
		gw.logError(path, err)
	}
	return errNo
}

func (gw *goWrapper) Rmdir(path string) errNo {
	defer gw.systemLock.CreateOrDelete(path)()
	if remover, ok := gw.FS.(filesystem.RemoveFS); ok {
		goPath, err := fuseToGo(path)
		if err != nil {
			return interpretError(err)
		}
		if err := remover.Remove(goPath); err != nil {
			return interpretError(err)
		}
		return operationSuccess
	}
	return -fuse.ENOSYS
}

func (ds *directoryStream) opened() bool {
	return ds.ReadDirFile != nil && ds.CancelFunc != nil
}

func (ds *directoryStream) Close() error {
	if !ds.opened() {
		return errDirStreamNotOpened
	}
	ds.CancelFunc()
	ds.CancelFunc = nil
	ds.entries = nil
	return ds.ReadDirFile.Close()
}
