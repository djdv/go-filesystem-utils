package cgofuse

import (
	"fmt"
	"io"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/winfsp/cgofuse/fuse"
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

func (hb *goWrapper) Open(path string, flags int) (int, uint64) {
	defer hb.systemLock.Access(path)()
	hb.log.Printf("Open - {%X}%q", flags, path)

	// TODO: this when OpenDir is implimented
	// if path == posixRoot {
	//fs.log.Print(fuselib.Error(-fuselib.EISDIR))
	//return -fuselib.EISDIR, errorHandle
	//}

	goPath, err := fuseToGo(path)
	if err != nil {
		// TODO: review; POSIX spec - make sure errno is appropriate for this op
		hb.log.Print(err)
		return interpretError(err), errorHandle
	}

	// TODO: port flags and use OpenFile
	// file, err := fs.goFs.Open(path, ioFlagsFromFuse(flags))
	file, err := hb.FS.Open(goPath)
	if err != nil {
		hb.log.Print(err)
		return interpretError(err), errorHandle
	}

	handle, err := hb.fileTable.Add(file)
	if err != nil {
		hb.log.Print(fuse.Error(-fuse.EMFILE))
		return -fuse.EMFILE, errorHandle
	}

	return operationSuccess, handle
}

func (fs *goWrapper) Release(path string, fh uint64) int {
	fs.log.Printf("Release - {%X}%q", fh, path)

	errNo, err := releaseFile(fs.fileTable, fh)
	if err != nil {
		fs.log.Print(err)
	}

	return errNo
}

func (hb *goWrapper) Read(path string, buff []byte, ofst int64, fh uint64) int {
	defer hb.systemLock.Access(path)()
	hb.log.Printf("Read {%X|%d}%q", fh, ofst, path)

	// TODO: [review] we need to do things on failure
	// the OS typically triggers a close, but we shouldn't expect it to invalidate this record for us
	// we also might want to store a file cursor to reduce calls to seek
	// the same thing already happens internally so it's at worst the overhead of a call right now

	file, err := hb.fileTable.Get(fh)
	if err != nil {
		hb.log.Print(fuse.Error(-fuse.EBADF))
		return -fuse.EBADF
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
		hb.log.Print(err)
		return -fuse.EIO
	}

	retVal, err := readFile(fsFile, buff, ofst)
	if err != nil && err != io.EOF {
		hb.log.Print(err)
	}
	return retVal
}

func (hb *goWrapper) Write(path string, buff []byte, ofst int64, fh uint64) int {
	defer hb.systemLock.Modify(path)()
	hb.log.Printf("Write - HostRequest {%X|%d|%d}%q", fh, len(buff), ofst, path)
	return -fuse.EROFS

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
