package cgofuse

import (
	"errors"
	"fmt"
	"io"
	"io/fs"

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
	errNo, err := releaseFile(fs.fileTable, fh)
	if err != nil {
		fs.log.Print(err)
	}
	return errNo
}

func (fsys *goWrapper) Read(path string, buff []byte, ofst int64, fh uint64) int {
	defer fsys.systemLock.Access(path)()

	handle, err := fsys.fileTable.Get(fh)
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
