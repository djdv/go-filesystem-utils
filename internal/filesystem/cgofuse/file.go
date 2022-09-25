package cgofuse

import (
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/billziss-gh/cgofuse/fuse"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/u-root/uio/ulog"
)

type Fuse struct {
	*fuse.FileSystemHost
}

func (fh Fuse) Close() error {
	if !fh.Unmount() {
		// TODO: we should store the target + whatever else
		// so we can print out a more helpful message here.
		// TODO: investigate forking fuselib so that it returns us the same error
		// it throws to the system's logger.
		return fmt.Errorf("unmount failed - system log may have more information")
	}
	return nil
}

// TODO: types
// TODO: signature / interface may need to change. We're going to want extensions to FS,
// and we have to decide if we want to use Go standard FS form, or explicitly typed interfaces.
func MountFuse(fsys fs.FS, target string) (io.Closer, error) {
	fuse, err := GoToFuse(fsys)
	if err != nil {
		return nil, err
	}

	var fsid filesystem.ID
	// TODO: define this interface within [filesystem] pkg.
	if idFS, ok := fsys.(interface {
		ID() filesystem.ID
	}); ok {
		fsid = idFS.ID()
	}

	return fuse, AttachToHost(fuse.FileSystemHost, fsid, target)
}

func GoToFuse(fs fs.FS) (Fuse, error) {
	fsh := fuse.NewFileSystemHost(&goWrapper{
		FS:         fs,
		fileTable:  newFileTable(),
		systemLock: newOperationsLock(),
		log:        ulog.Null, // TODO: from options
	})
	// TODO: from options.
	fsh.SetCapReaddirPlus(canReaddirPlus)
	fsh.SetCapCaseInsensitive(false)
	//
	return Fuse{FileSystemHost: fsh}, nil
	// TODO: WithLog(...) option.
	// var eLog logging.EventLogger
	// if idFs, ok := fs.(filesystem.IdentifiedFS); ok {
	// 	eLog = log.New(idFs.ID().String())
	// } else {
	// 	eLog = log.New("ipfs-core")
	// }

	// sysLog := ulog.Null
	// const logStub = false // TODO: from CLI flags / funcopts.
	// if logStub {
	// 	// sysLog = log.Default()
	// 	sysLog = log.New(os.Stdout, "fuse dbg - ", log.Lshortfile)
	// }
	// return &hostBinding{
	// 	goFs: fs,
	// 	log:  sysLog,
	// }
}

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

// TODO: inline this? - if not, we may need to split it if Open and OpenDir expect different
func releaseFile(table fileTable, handle uint64) (errNo, error) {
	file, err := table.Get(handle)
	if err != nil {
		return -fuse.EBADF, err
	}

	// SUSv7 `close` (parphrased)
	// if errors are encountered, the result of the handle is unspecified
	// for us specifically, we'll remove the handle regardless of its close return

	if err := table.Remove(handle); err != nil {
		// TODO: if the error is not found we need to panic or return a severe error
		// this should not be possible
		return -fuse.EBADF, err
	}

	return operationSuccess, file.goFile.Close()
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

// TODO: read+write; we're not accounting for scenarios where the offset is beyond the end of the file
func readFile(file filesystem.File, buff []byte, ofst int64) (errNo, error) {
	if len(buff) == 0 {
		return 0, nil
	}

	if ofst < 0 {
		return -fuse.EINVAL, fmt.Errorf("invalid offset %d", ofst)
	}

	// TODO: file.ReaderAt support?

	stat, err := file.Stat()
	if err != nil {
		// TODO: consult spec for correct error value - this one is temporary
		return -fuse.EIO, err
	}

	if ofst >= stat.Size() {
		return 0, nil // POSIX expects this
	}

	if _, err := file.Seek(ofst, io.SeekStart); err != nil {
		return -fuse.EIO, err
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
				readBytes = -fuse.EIO
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
