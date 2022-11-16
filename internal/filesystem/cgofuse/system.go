package cgofuse

import (
	"errors"
	"fmt"
	"io"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/winfsp/cgofuse/fuse"
)

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
