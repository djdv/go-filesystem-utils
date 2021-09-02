package cgofuse

import (
	"fmt"
	"io"
	"io/fs"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
)

//TODO: dbg only - (re)move this
/*
func (fs *hostBinding) Readlink(path string) (int, string) {
	fs.log.Debugf("Readlink - %q", path)

	switch path {
	case "/":
		fs.log.Warnf("Readlink - root path is an invalid Request")
		return -fuselib.EINVAL, ""

	case "":
		fs.log.Error("Readlink - empty Request")
		return -fuselib.ENOENT, ""
	}
	return -fuselib.ENOSYS, ""
}
*/

func (fs *hostBinding) Open(path string, flags int) (int, uint64) {
	fs.log.Debugf("Open - {%X}%q", flags, path)

	// TODO: this when OpenDir is implimented
	// if path == posixRoot {
	//fs.log.Error(fuselib.Error(-fuselib.EISDIR))
	//return -fuselib.EISDIR, errorHandle
	//}

	goPath, err := posixToGo(path)
	if err != nil {
		// TODO: review; POSIX spec - make sure errno is appropriate for this op
		fs.log.Error(err)
		return interpretError(err), errorHandle
	}

	// TODO: port flags and use OpenFile
	//file, err := fs.goFs.Open(path, ioFlagsFromFuse(flags))
	file, err := fs.goFs.Open(goPath)
	if err != nil {
		fs.log.Error(err)
		return interpretError(err), errorHandle
	}

	handle, err := fs.fileTable.Add(file)
	if err != nil {
		fs.log.Error(fuselib.Error(-fuselib.EMFILE))
		return -fuselib.EMFILE, errorHandle
	}

	return operationSuccess, handle
}

func (fs *hostBinding) Release(path string, fh uint64) int {
	fs.log.Debugf("Release - {%X}%q", fh, path)

	errNo, err := releaseFile(fs.fileTable, fh)
	if err != nil {
		fs.log.Error(err)
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

	return operationSuccess, file.Close()
}

func (fs *hostBinding) Read(path string, buff []byte, ofst int64, fh uint64) int {
	fs.log.Debugf("Read - HostRequest {%X|%d}%q", fh, ofst, path)

	// TODO: [review] we need to do things on failure
	// the OS typically triggers a close, but we shouldn't expect it to invalidate this record for us
	// we also might want to store a file cursor to reduce calls to seek
	// the same thing already happens internally so it's at worst the overhead of a call right now

	file, err := fs.fileTable.Get(fh)
	if err != nil {
		fs.log.Error(fuselib.Error(-fuselib.EBADF))
		return -fuselib.EBADF
	}

	retVal, err := readFile(file, buff, ofst)
	if err != nil && err != io.EOF {
		fs.log.Error(err)
	}
	return retVal
}

// TODO: read+write; we're not accounting for scenarios where the offset is beyond the end of the file
func readFile(file fs.File, buff []byte, ofst int64) (errNo, error) {
	if len(buff) == 0 {
		return 0, nil
	}

	if ofst < 0 {
		return -fuselib.EINVAL, fmt.Errorf("invalid offset %d", ofst)
	}

	// TODO: consult spec for correct error value - this one is temporary
	stat, err := file.Stat()
	if err != nil {
		return -fuselib.EIO, err
	}

	if ofst >= stat.Size() {
		return 0, nil // POSIX expects this
	}

	// TODO: quick hack; do properly
	// (consider if we want to use generic type+assert or hard type internally, cast during Open)
	seekerFile, ok := file.(io.Seeker)
	if !ok {
		panic("TODO: real Unix error value goes here")
	}

	if _, err := seekerFile.Seek(ofst, io.SeekStart); err != nil {
		return -fuselib.EIO, err
	}

	readBytes, err := file.Read(buff)
	if err != nil && err != io.EOF {
		readBytes = -fuselib.EIO // POSIX overloads this variable; at this point it becomes an error
	}

	// TODO: [Ame] spec-note+English
	// NOTE: we don't have to about `io.Reader` using memory beyond `buff[readBytes:]
	// (because of POSIX `read` semantics,
	// the caller should except bytes beyond `readBytes` to be garbage data anyway)

	return readBytes, err // EOF will be returned if it was provided
}

func (fs *hostBinding) Write(path string, buff []byte, ofst int64, fh uint64) int {
	fs.log.Debugf("Write - HostRequest {%X|%d|%d}%q", fh, len(buff), ofst, path)

	if path == "/" { // root Request; we're never a file
		fs.log.Error(fuselib.Error(-fuselib.EBADF))
		return -fuselib.EBADF
	}

	file, err := fs.fileTable.Get(fh)
	if err != nil {
		fs.log.Error(fuselib.Error(-fuselib.EBADF))
		return -fuselib.EBADF
	}

	errNo, err := writeFile(file, buff, ofst)
	if err != nil && err != io.EOF {
		fs.log.Error(err)
	}
	return errNo
}

func writeFile(file fs.File, buff []byte, ofst int64) (errNo, error) {
	if len(buff) == 0 {
		return 0, nil
	}

	if ofst < 0 {
		return -fuselib.EINVAL, fmt.Errorf("invalid offset %d", ofst)
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
