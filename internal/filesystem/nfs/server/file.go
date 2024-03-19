package nfs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

type (
	file struct {
		goFile    fs.File
		billyName string // "… as presented to Open." -v5 docs.
	}
	// fileEx extends operation support
	// of basic [fs.File]s.
	fileEx struct {
		file
		curMu sync.Mutex
	}
)

func (nf *file) Name() string { return nf.billyName }

func (nf *file) Write(p []byte) (n int, err error) {
	if writer, ok := nf.goFile.(io.Writer); ok {
		return writer.Write(p)
	}
	const op = "write"
	return -1, unsupportedOpErr(op, nf.billyName)
}

func (nf *file) Read(p []byte) (int, error) {
	return nf.goFile.Read(p)
}

func (nf *file) ReadAt(p []byte, off int64) (int, error) {
	// NOTE: interface checked during [Open].
	return nf.goFile.(io.ReaderAt).ReadAt(p, off)
}

func (nf *fileEx) Read(p []byte) (int, error) {
	nf.curMu.Lock()
	defer nf.curMu.Unlock()
	return nf.file.Read(p)
}

func (nf *fileEx) ReadAt(p []byte, off int64) (int, error) {
	nf.curMu.Lock()
	defer nf.curMu.Unlock()
	readSeeker, ok := nf.goFile.(io.ReadSeeker)
	if !ok {
		const op = "readat"
		return -1, unsupportedOpErr(op, nf.billyName)
	}
	return readAtLocked(readSeeker, p, off)
}

func readAtLocked(rs io.ReadSeeker, p []byte, off int64) (int, error) {
	const errno = -1
	were, err := rs.Seek(0, io.SeekCurrent)
	if err != nil {
		return errno, err
	}
	sought, err := rs.Seek(off, io.SeekStart)
	if err != nil {
		return errno, err
	}
	if err := compareOffsets(sought, off); err != nil {
		return errno, err
	}
	n, rErr := rs.Read(p)
	where, err := rs.Seek(were, io.SeekStart)
	if err != nil {
		return errno, errors.Join(err, rErr)
	}
	if err := compareOffsets(where, were); err != nil {
		return errno, errors.Join(err, rErr)
	}
	return n, rErr
}

func (nf *file) Seek(offset int64, whence int) (int64, error) {
	if seeker, ok := nf.goFile.(io.Seeker); ok {
		return seeker.Seek(offset, whence)
	}
	const op = "seek"
	return -1, unsupportedOpErr(op, nf.billyName)
}

func (nf *file) Close() error {
	return nf.goFile.Close()
}

func (nf *file) Lock() error {
	const op = "lock"
	return unsupportedOpErr(op, nf.billyName)
}

func (nf *file) Unlock() error {
	const op = "unlock"
	return unsupportedOpErr(op, nf.billyName)
}

func (nf *file) Truncate(size int64) error {
	if truncater, ok := nf.goFile.(filesystem.TruncateFile); ok {
		return truncater.Truncate(size)
	}
	const op = "truncate"
	return unsupportedOpErr(op, nf.billyName)
}

func unsupportedOpErr(op, name string) error {
	return fmt.Errorf(
		op+` "%s": %w`,
		name, errors.ErrUnsupported,
	)
}

func compareOffsets(got, want int64) (err error) {
	if got == want {
		return nil
	}
	return fmt.Errorf(
		"offset mismatch got %d expected %d",
		got, want,
	)
}
