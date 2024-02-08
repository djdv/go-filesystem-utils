package cgofuse

import (
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/winfsp/cgofuse/fuse"
)

func (gw *goWrapper) Create(path string, flags int, mode uint32) (errNo, fileDescriptor) {
	defer gw.systemLock.CreateOrDelete(path)()
	name, err := fuseToGo(path)
	if err != nil {
		gw.logError(path, err)
		return interpretError(err), errorHandle
	}
	var (
		fsFlags     = goFlagsFromFuse(flags)
		permissions = fuseToGoPermissions(mode)
	)
	file, err := filesystem.OpenFile(gw.FS, name, fsFlags, permissions)
	if err != nil {
		gw.logError(path, err)
		return interpretError(err), errorHandle
	}
	handle, err := gw.fileTable.add(file)
	if err != nil {
		gw.logError(path, err)
		return -fuse.EMFILE, errorHandle
	}
	return operationSuccess, handle
}

func (gw *goWrapper) exists(name string) bool {
	_, err := fs.Stat(gw.FS, name)
	if err == nil {
		return true
	}
	var fsErr *fserrors.Error
	if errors.As(err, &fsErr) {
		return fsErr.Kind != fserrors.NotExist
	}
	return true
}

func (gw *goWrapper) Mknod(path string, mode uint32, dev uint64) errNo {
	defer gw.systemLock.CreateOrDelete(path)()
	name, err := fuseToGo(path)
	if err != nil {
		gw.logError(path, err)
		return interpretError(err)
	}
	if gw.exists(name) {
		return -fuse.EEXIST
	}
	if creator, ok := gw.FS.(filesystem.CreateFileFS); ok {
		file, err := creator.CreateFile(name)
		if err != nil {
			gw.logError(path, err)
			return interpretError(err)
		}
		if err := file.Close(); err != nil {
			gw.logError(path, err)
			return interpretError(err)
		}
		return operationSuccess
	}
	return -fuse.ENOSYS
}

func (gw *goWrapper) Truncate(path string, size int64, fh fileDescriptor) errNo {
	defer gw.systemLock.Modify(path)()
	if size < 0 {
		const errNo = -fuse.EINVAL
		gw.logError(path, fuse.Error(errNo))
		return errNo
	}
	// TODO: [metadata] "Unless FUSE_CAP_HANDLE_KILLPRIV is disabled,
	// this method is expected to reset the setuid and setgid bits."
	var (
		errNo errNo
		opErr error
	)
	switch handle, err := gw.fileTable.get(fh); {
	case err == nil:
		errNo, opErr = truncateFile(handle.goFile, size)
	case errors.Is(err, errInvalidHandle):
		errNo, opErr = truncatePath(gw.FS, path, size)
	default:
		errNo, opErr = interpretError(err), err
	}
	if opErr != nil {
		gw.logError(path, opErr)
	}
	return errNo
}

func truncateFile(file fs.File, size int64) (errNo, error) {
	truncater, ok := file.(filesystem.TruncateFile)
	if !ok {
		return -fuse.ENOSYS, fmt.Errorf("%T does not implement truncate", file)
	}
	if err := truncater.Truncate(size); err != nil {
		return interpretError(err), err
	}
	return operationSuccess, nil
}

func truncatePath(fsys fs.FS, path string, size int64) (errNo, error) {
	name, err := fuseToGo(path)
	if err != nil {
		// TODO: review; POSIX spec - make sure errno is appropriate for this op
		// EINVAL refers to size, not path in this context.
		// So we don't call [interpretError].
		return -fuse.EACCES, err
	}
	if err := filesystem.Truncate(fsys, name, size); err != nil {
		// TODO: [filesystem] should have defined error values for us
		// to hook on. We have to manually check for now.
		var fsErr *fserrors.Error
		if errors.As(err, &fsErr) {
			return fsErrorsTable[fsErr.Kind], err
		}
		return -fuse.ENOSYS, err
	}
	return operationSuccess, nil
}

func (gw *goWrapper) Open(path string, flags int) (errNo, fileDescriptor) {
	if flags&fuse.O_TRUNC != 0 {
		defer gw.systemLock.Modify(path)()
	} else {
		defer gw.systemLock.Access(path)()
	}
	if path == mountedFusePath {
		// Special case; see: [pollMountpoint].
		return operationSuccess, errorHandle
	}

	name, err := fuseToGo(path)
	if err != nil {
		gw.logError(path, err)
		return interpretError(err), errorHandle
	}

	const permissions = 0
	fsFlags := goFlagsFromFuse(flags)
	file, err := filesystem.OpenFile(gw.FS, name, fsFlags, permissions)
	if err != nil {
		gw.logError(path, err)
		return interpretError(err), errorHandle
	}

	handle, err := gw.fileTable.add(file)
	if err != nil {
		gw.logError(path, err)
		return -fuse.EMFILE, errorHandle
	}

	return operationSuccess, handle
}

func (gw *goWrapper) Write(path string, buff []byte, ofst int64, fh fileDescriptor) int {
	defer gw.systemLock.Modify(path)()
	handle, err := gw.fileTable.get(fh)
	if err != nil {
		gw.logError(path, err)
		return -fuse.EBADF
	}
	handle.ioMu.Lock()
	defer handle.ioMu.Unlock()

	// TODO: Handle access checks. O_WRONLY|O_RDWR

	errNo, err := writeFile(handle.goFile, buff, ofst)
	if err != nil {
		gw.logError(path, err)
	}
	return errNo
}

func writeFile(file fs.File, buff []byte, ofst int64) (errNo, error) {
	if ofst < 0 {
		return -fuse.EINVAL, fmt.Errorf("invalid offset %d", ofst)
	}

	writer, ok := file.(io.Writer)
	if !ok { // Access should have been be checked during [Open] with `EROFS` returned.
		return -fuse.EIO, fmt.Errorf("%T does not support writing", file)
	}

	errNo, err := seekFile(file, ofst)
	if err != nil {
		return errNo, err
	}

	wroteBytes, err := writer.Write(buff)
	if err != nil {
		return -fuse.EIO, err
	}
	return wroteBytes, nil
}

func (gw *goWrapper) Fsync(path string, datasync bool, fh fileDescriptor) errNo {
	defer gw.systemLock.Modify(path)()
	return -fuse.ENOSYS
}

func (gw *goWrapper) Read(path string, buff []byte, ofst int64, fh fileDescriptor) int {
	defer gw.systemLock.Access(path)()

	handle, err := gw.fileTable.get(fh)
	if err != nil {
		gw.logError(path, err)
		return -fuse.EBADF
	}
	handle.ioMu.Lock()
	defer handle.ioMu.Unlock()

	retVal, err := readFile(handle.goFile, buff, ofst)
	if err != nil {
		gw.logError(path, err)
	}
	return retVal
}

func readFile(file fs.File, buff []byte, ofst int64) (int, error) {
	if ofst < 0 {
		return -fuse.EINVAL, fmt.Errorf("invalid offset %d", ofst)
	}

	errNo, err := seekFile(file, ofst)
	if err != nil {
		return errNo, err
	}

	n, err := io.ReadFull(file, buff)
	if err != nil {
		isEOF := errors.Is(err, io.EOF) ||
			errors.Is(err, io.ErrUnexpectedEOF)
		if !isEOF {
			return -fuse.EIO, err
		}
	}
	return n, nil
}

func seekFile(file fs.File, ofst int64) (errNo, error) {
	if _, err := filesystem.Seek(file, ofst, io.SeekStart); err != nil {
		if errors.Is(err, errors.ErrUnsupported) {
			return -fuse.ESPIPE, err
		}
		return -fuse.EIO, err
	}
	return operationSuccess, nil
}

func (gw *goWrapper) Release(path string, fh fileDescriptor) errNo {
	errNo, err := gw.fileTable.release(fh)
	if err != nil {
		gw.logError(path, err)
	}
	return errNo
}
