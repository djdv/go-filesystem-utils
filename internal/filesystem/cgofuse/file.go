//go:build !nofuse

package cgofuse

import (
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/winfsp/cgofuse/fuse"
)

func (gw *goWrapper) Create(path string, flags int, mode uint32) (int, uint64) {
	// FIXME: lock here, call common locked open method.
	// gw.log.Warnf("Create - {%X|%X}%q", flags, mode, path)

	// TODO: why is fuselib passing us flags and what are they?
	// both FUSE and SUS predefine what they should be (to Open)
	// ^ this is likely the filtering process.
	// syscall `open` with flags `O_CREAT` will (likely) be defered to us
	// where flags are whatever they were in the call to `open`
	// except with `O_CREAT` masked out.
	// This needs a C test to verify.
	gw.log.Printf("Create - {%X|%X}%q", flags, mode, path)
	return gw.Open(path, flags)
	// return gw.Open(path, fuse.O_WRONLY|fuse.O_CREAT|fuse.O_TRUNC)

	// disabled until we parse relevant flags in open
	// fuse will do shenanigans to make this work
	// return -fuse.ENOSYS, errorHandle
}

// TODO: move these methods
func (gw *goWrapper) Mknod(path string, mode uint32, dev uint64) int {
	defer gw.systemLock.CreateOrDelete(path)()
	gw.log.Print("mknod:", path)
	if maker, ok := gw.FS.(filesystem.MakeFileFS); ok {
		goPath, err := fuseToGo(path)
		if err != nil {
			return interpretError(err)
		}
		if err := maker.MakeFile(goPath); err != nil {
			return interpretError(err)
		}
		return operationSuccess
	}
	return -fuse.ENOSYS
}

func (gw *goWrapper) Truncate(path string, size int64, fh uint64) int {
	defer gw.systemLock.Modify(path)()
	gw.log.Printf("Truncate - HostRequest {%X|%d}%q", fh, size, path)
	if size < 0 {
		return -fuse.EINVAL
	}

	var didOpen bool
	file, err := gw.fileTable.get(fh) // use the handle if it's valid
	if err != nil {                   // otherwise fallback to open
		goPath, err := fuseToGo(path)
		if err != nil {
			// TODO: review; POSIX spec - make sure errno is appropriate for this op
			// hb.log.Print(err)
			return interpretError(err)
		}
		// TODO: call OpenFile with truncate flag.
		goFile, err := gw.FS.Open(goPath)
		if err != nil {
			// gw.log.Error(err)
			return interpretError(err)
		}
		didOpen = true
		file = &fileHandle{ // TODO: lazy port; we don't need to wrap this.
			goFile: goFile,
		}
	}

	/*
		if err = file.Truncate(uint64(size)); err != nil {
			gw.log.Error(err)
			return interpretError(err)
		}
	*/

	truncater, ok := file.goFile.(filesystem.TruncateFile)
	if !ok { // TODO: quick port; this should be done properly. [lazy]
		if didOpen {
			if err := file.goFile.Close(); err != nil {
				// gw.log.Error(err)
				return interpretError(err)
			}
		}
		// TODO: what value should we use?
		// EPERM and EROFS both seem applicable.
		// The latter might have have unintended side effects though.
		// EINVAL might be best even if this is a "regular" file.
		return -fuse.EINVAL
	}

	if err := truncater.Truncate(uint64(size)); err != nil {
		return interpretError(err)
	}

	if didOpen {
		if err := file.goFile.Close(); err != nil {
			// gw.log.Error(err)
			return interpretError(err)
		}
	}
	return operationSuccess
}

func (gw *goWrapper) Open(path string, flags int) (int, uint64) {
	defer gw.systemLock.Access(path)()
	gw.log.Printf("Open - {%X}%q", flags, path)

	// TODO: this when OpenDir is implimented
	// if path == posixRoot {
	//fs.log.Print(fuselib.Error(-fuselib.EISDIR))
	//return -fuselib.EISDIR, errorHandle
	//}

	goPath, err := fuseToGo(path)
	if err != nil {
		// TODO: review; POSIX spec - make sure errno is appropriate for this op
		gw.log.Print(err)
		return interpretError(err), errorHandle
	}

	// file, err := gw.FS.Open(goPath)
	// TODO: [S&P] double check compliance.
	// a correct FUSE implementation should never pass `O_CREAT` to `open`
	// this is a deviation from POSIX `open`.
	// Thus the permission value /should/ always go unused in Go's `OpenFile`.
	// We need C tests to validate that syscall `open` with these flags
	// does not actually call us. (It likely gets defered to [Create] by fuse)
	const permissions = 0
	goFlags := goFlagsFromFuse(flags)
	file, err := filesystem.OpenFile(gw.FS, goPath, goFlags, permissions)
	if err != nil {
		gw.log.Print(err)
		return interpretError(err), errorHandle
	}

	handle, err := gw.fileTable.add(file)
	if err != nil {
		gw.log.Print(fuse.Error(-fuse.EMFILE))
		return -fuse.EMFILE, errorHandle
	}

	return operationSuccess, handle
}

func (gw *goWrapper) Write(path string, buff []byte, ofst int64, fh uint64) int {
	defer gw.systemLock.Modify(path)()
	if path == "/" { // root Request; we're never a file
		// gw.log.Print(fuse.Error(-fuse.EBADF))
		return -fuse.EBADF
	}

	file, err := gw.fileTable.get(fh)
	if err != nil {
		// gw.log.Print(fuselib.Error(-fuselib.EBADF))
		return -fuse.EBADF
	}

	errNo, err := writeFile(file.goFile, buff, ofst)
	if err != nil && err != io.EOF {
		gw.log.Print(err)
	}
	return errNo
}

// TODO: inline
func writeFile(file fs.File, buff []byte, ofst int64) (errNo, error) {
	if len(buff) == 0 {
		return 0, nil
	}

	if ofst < 0 {
		return -fuse.EINVAL, fmt.Errorf("invalid offset %d", ofst)
	}

	/* TODO: test this; it should be handled internally by seek()+write()
	if not, uncomment, if so, remove

	if fileBound, err := file.Size(); err == nil {
		if ofst >= fileBound {
			newEnd := fileBound - (ofst - int64(len(buff)))
			if err := file.Truncate(uint64(newEnd)); err != nil { // pad 0's before our write
				return err, -fuselib.EIO
			}
		}
	}
	*/

	// TODO: quick hack; do properly
	// (consider if we want to use generic type+assert or hard type internally, cast during Open)
	seekerFile, ok := file.(io.Seeker)
	if !ok {
		panic("TODO: real Unix error value goes here")
	}

	if _, err := seekerFile.Seek(ofst, io.SeekStart); err != nil {
		return -fuse.EIO, fmt.Errorf("offset seek error: %s", err)
	}

	// TODO: quick hack; do properly
	// (consider if we want to use generic type+assert or hard type internally, cast during Open)
	writerFile, ok := file.(io.Writer)
	if !ok {
		panic("TODO: real Unix error value goes here")
	}

	wroteBytes, err := writerFile.Write(buff)
	if err != nil {
		return -fuse.EIO, err
	}

	return wroteBytes, nil
}

func (fs *goWrapper) Fsync(path string, datasync bool, fh uint64) int {
	fs.log.Printf("Fsync {%X|%t}%q", fh, datasync, path)
	return -fuse.ENOSYS
}

func (fsys *goWrapper) Read(path string, buff []byte, ofst int64, fh uint64) int {
	defer fsys.systemLock.Access(path)()

	handle, err := fsys.fileTable.get(fh)
	if err != nil {
		fsys.log.Print(err)
		return -fuse.EBADF
	}
	handle.ioMu.Lock()
	defer handle.ioMu.Unlock()

	retVal, err := readFile(handle.goFile, buff, ofst)
	if err != nil {
		fsys.log.Printf("%s - %s", err, path)
	}
	return retVal
}

func readFile(file fs.File, buff []byte, ofst int64) (int, error) {
	seekerFile, ok := file.(seekerFile)
	if !ok {
		return -fuse.EIO, errors.New("file does not implement seeking")
	}
	if len(buff) == 0 {
		return 0, nil
	}
	if ofst < 0 {
		return -fuse.EINVAL, fmt.Errorf("invalid offset %d", ofst)
	}
	stat, err := file.Stat()
	if err != nil {
		return -fuse.EIO, err
	}
	if ofst >= stat.Size() {
		return 0, nil
	}
	if _, err := seekerFile.Seek(ofst, io.SeekStart); err != nil {
		return -fuse.EIO, err
	}
	n, err := io.ReadFull(file, buff)
	if err != nil {
		isEof := errors.Is(err, io.EOF) ||
			errors.Is(err, io.ErrUnexpectedEOF)
		if !isEof {
			return -fuse.EIO, err
		}
	}
	return n, nil
}

func (fs *goWrapper) Release(path string, fh uint64) int {
	errNo, err := fs.fileTable.release(fh)
	if err != nil {
		fs.log.Print(err)
	}
	return errNo
}
