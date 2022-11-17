package cgofuse

import (
	"github.com/winfsp/cgofuse/fuse"
)

type ()

// TODO: inline this? - if not, we may need to split it if Open and OpenDir expect different
func releaseFile(table fileTable, handle uint64) (errNo, error) {
	file, err := table.Get(handle)
	if err != nil {
		return -fuse.EBADF, err
	}

	// SUSv7 `close` (paraphrased)
	// if errors are encountered, the result of the handle is unspecified
	// for us specifically, we'll remove the handle regardless of its close return

	if err := table.Remove(handle); err != nil {
		// TODO: if the error is not found we need to panic or return a severe error
		// this should not be possible
		return -fuse.EBADF, err
	}

	return operationSuccess, file.goFile.Close()
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
