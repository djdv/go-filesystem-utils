package cgofuse

import (
	"errors"
	"fmt"
	"io"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

//TODO: dbg only - (re)move this
/*
func (fs *hostBinding) Readlink(path string) (int, string) {
	fs.log.Printf("Readlink - %q", path)

	switch path {
	case "/":
		fs.log.Warnf("Readlink - root path is an invalid Request")
		return -fuselib.EINVAL, ""

	case "":
		fs.log.Print("Readlink - empty Request")
		return -fuselib.ENOENT, ""
	}
	return -fuselib.ENOSYS, ""
}
*/

func (fuse *hostBinding) Open(path string, flags int) (int, uint64) {
	defer fuse.systemLock.Access(path)()
	fuse.log.Printf("Open - {%X}%q", flags, path)

	// TODO: this when OpenDir is implimented
	// if path == posixRoot {
	//fs.log.Print(fuselib.Error(-fuselib.EISDIR))
	//return -fuselib.EISDIR, errorHandle
	//}

	goPath, err := fuseToGo(path)
	if err != nil {
		// TODO: review; POSIX spec - make sure errno is appropriate for this op
		fuse.log.Print(err)
		return interpretError(err), errorHandle
	}

	// TODO: port flags and use OpenFile
	// file, err := fs.goFs.Open(path, ioFlagsFromFuse(flags))
	file, err := fuse.goFs.Open(goPath)
	if err != nil {
		fuse.log.Print(err)
		return interpretError(err), errorHandle
	}

	handle, err := fuse.fileTable.Add(file)
	if err != nil {
		fuse.log.Print(fuselib.Error(-fuselib.EMFILE))
		return -fuselib.EMFILE, errorHandle
	}

	return operationSuccess, handle
}

func (fs *hostBinding) Release(path string, fh uint64) int {
	fs.log.Printf("Release - {%X}%q", fh, path)

	errNo, err := releaseFile(fs.fileTable, fh)
	if err != nil {
		fs.log.Print(err)
	}

	return errNo
}

// TODO: inline this? - if not, we may need to split it if Open and OpenDir expect different
func releaseFile(table fileTable, handle uint64) (errNo, error) {
	file, err := table.Get(handle)
	if err != nil {
		return -fuselib.EBADF, err
	}

	// SUSv7 `close` (parphrased)
	// if errors are encountered, the result of the handle is unspecified
	// for us specifically, we'll remove the handle regardless of its close return

	if err := table.Remove(handle); err != nil {
		// TODO: if the error is not found we need to panic or return a severe error
		// this should not be possible
		return -fuselib.EBADF, err
	}

	return operationSuccess, file.goFile.Close()
}

func (fuse *hostBinding) Read(path string, buff []byte, ofst int64, fh uint64) int {
	defer fuse.systemLock.Access(path)()
	fuse.log.Printf("Read {%X|%d}%q", fh, ofst, path)

	// TODO: [review] we need to do things on failure
	// the OS typically triggers a close, but we shouldn't expect it to invalidate this record for us
	// we also might want to store a file cursor to reduce calls to seek
	// the same thing already happens internally so it's at worst the overhead of a call right now

	file, err := fuse.fileTable.Get(fh)
	if err != nil {
		fuse.log.Print(fuselib.Error(-fuselib.EBADF))
		return -fuselib.EBADF
	}

	file.ioMu.Lock()
	defer file.ioMu.Unlock()

	fsFile, ok := file.goFile.(filesystem.File)
	if !ok {
		err := fmt.Errorf("file is unexpected type:"+
			"\n\tgot: %T"+
			"\n\twant: %T",
			file.goFile, fsFile,
		)
		fuse.log.Print(err)
		return -fuselib.EIO
	}

	retVal, err := readFile(fsFile, buff, ofst)
	if err != nil && err != io.EOF {
		fuse.log.Print(err)
	}
	return retVal
}

// TODO: read+write; we're not accounting for scenarios where the offset is beyond the end of the file
func readFile(file filesystem.File, buff []byte, ofst int64) (errNo, error) {
	if len(buff) == 0 {
		return 0, nil
	}

	if ofst < 0 {
		return -fuselib.EINVAL, fmt.Errorf("invalid offset %d", ofst)
	}

	// TODO: file.ReaderAt support?

	stat, err := file.Stat()
	if err != nil {
		// TODO: consult spec for correct error value - this one is temporary
		return -fuselib.EIO, err
	}

	if ofst >= stat.Size() {
		return 0, nil // POSIX expects this
	}

	if _, err := file.Seek(ofst, io.SeekStart); err != nil {
		return -fuselib.EIO, err
	}
	var (
		readBytes int
		// TODO: [Ame] English.
		// NOTE: [implementation:Fuse]
		// This slice points into memory allocated by Fuse.
		// (Implementation specific, but most likely a C void*)
		// Before it's passed to us, it's cast by Go, as a pointer to a byte array
		// the size of which should be considered arbitrary.
		// That array is then sliced and passed to us.
		// As a result, cap is forbidden in this function on that slice.
		// Memory in the range of [len+1:cap] is not actually allocated,
		// and writing to it will likely result in undefined behaviour.
		// (Most likely a segfault, or worse, memory corruption if unguarded by the runtime)
		bufferSize = len(buff)
	)
	for {
		n, err := file.Read(buff)
		readBytes += n
		if err != nil {
			if !errors.Is(err, io.EOF) {
				// POSIX overloads this variable; at this point it becomes an error
				readBytes = -fuselib.EIO
			}
			return readBytes, err
		}
		if readBytes == bufferSize {
			return readBytes, nil
		}
		buff = buff[n:]
	}

	// TODO: [Ame] spec-note+English
	// NOTE: we don't have to about `io.Reader` using memory beyond `buff[readBytes:]
	// (because of POSIX `read` semantics,
	// the caller should except bytes beyond `readBytes` to be garbage data anyway)
}

func (fuse *hostBinding) Write(path string, buff []byte, ofst int64, fh uint64) int {
	defer fuse.systemLock.Modify(path)()
	fuse.log.Printf("Write - HostRequest {%X|%d|%d}%q", fh, len(buff), ofst, path)
	return -fuselib.EROFS

	/*
		if path == "/" { // root Request; we're never a file
			fuse.log.Print(fuselib.Error(-fuselib.EBADF))
			return -fuselib.EBADF
		}

		file, err := fuse.fileTable.Get(fh)
		if err != nil {
			fuse.log.Print(fuselib.Error(-fuselib.EBADF))
			return -fuselib.EBADF
		}

		errNo, err := writeFile(file.goFile, buff, ofst)
		if err != nil && err != io.EOF {
			fuse.log.Print(err)
		}
		return errNo
	*/
}

/*
func writeFile(file fs.File, buff []byte, ofst int64) (errNo, error) {
	if len(buff) == 0 {
		return 0, nil
	}

	if ofst < 0 {
		return -fuselib.EINVAL, fmt.Errorf("invalid offset %d", ofst)
	}

	//TODO: test this; it should be handled internally by seek()+write()
	//if not, uncomment, if so, remove

	//if fileBound, err := file.Size(); err == nil {
	//	if ofst >= fileBound {
	//		newEnd := fileBound - (ofst - int64(len(buff)))
	//		if err := file.Truncate(uint64(newEnd)); err != nil { // pad 0's before our write
	//			return err, -fuselib.EIO
	//		}
	//	}
	//}

	// TODO: quick hack; do properly
	// (consider if we want to use generic type+assert or hard type internally, cast during Open)
	seekerFile, ok := file.(io.Seeker)
	if !ok {
		panic("TODO: real Unix error value goes here")
	}

	if _, err := seekerFile.Seek(ofst, io.SeekStart); err != nil {
		return -fuselib.EIO, fmt.Errorf("offset seek error: %s", err)
	}

	// TODO: quick hack; do properly
	// (consider if we want to use generic type+assert or hard type internally, cast during Open)
	writerFile, ok := file.(io.Writer)
	if !ok {
		panic("TODO: real Unix error value goes here")
	}

	wroteBytes, err := writerFile.Write(buff)
	if err != nil {
		return -fuselib.EIO, err
	}

	return wroteBytes, nil
}
*/
