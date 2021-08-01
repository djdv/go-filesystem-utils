//+build !nofuse

package cgofuse

import (
	"io"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
)

func (fs *hostBinding) Open(path string, flags int) (int, uint64) {
	fs.log.Debugf("Open - {%X}%q", flags, path)

	switch path {
	case "":
		fs.log.Error(fuselib.Error(-fuselib.ENOENT))
		return -fuselib.ENOENT, errorHandle
	case "/":
		fs.log.Error(fuselib.Error(-fuselib.EISDIR))
		return -fuselib.EISDIR, errorHandle
	}

	file, err := fs.nodeInterface.Open(path, ioFlagsFromFuse(flags))
	if err != nil {
		fs.log.Error(err)
		return interpretError(err), errorHandle
	}

	handle, err := fs.files.Add(file)
	if err != nil {
		fs.log.Error(fuselib.Error(-fuselib.EMFILE))
		return -fuselib.EMFILE, errorHandle
	}

	return operationSuccess, handle
}

func (fs *hostBinding) Release(path string, fh uint64) int {
	fs.log.Debugf("Release - {%X}%q", fh, path)

	errNo, err := releaseFile(fs.files, fh)
	if err != nil {
		fs.log.Error(err)
	}

	return errNo
}

func (fs *hostBinding) Read(path string, buff []byte, ofst int64, fh uint64) int {
	fs.log.Debugf("Read - HostRequest {%X|%d}%q", fh, ofst, path)

	// TODO: [review] we need to do things on failure
	// the OS typically triggers a close, but we shouldn't expect it to invalidate this record for us
	// we also might want to store a file cursor to reduce calls to seek
	// the same thing already happens internally so it's at worst the overhead of a call right now

	file, err := fs.files.Get(fh)
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

func (fs *hostBinding) Write(path string, buff []byte, ofst int64, fh uint64) int {
	fs.log.Debugf("Write - HostRequest {%X|%d|%d}%q", fh, len(buff), ofst, path)

	if path == "/" { // root Request; we're never a file
		fs.log.Error(fuselib.Error(-fuselib.EBADF))
		return -fuselib.EBADF
	}

	file, err := fs.files.Get(fh)
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
