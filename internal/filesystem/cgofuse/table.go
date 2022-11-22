//go:build !nofuse

package cgofuse

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"sync"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/winfsp/cgofuse/fuse"
)

type fileTable struct {
	sync.RWMutex
	files []*fileHandle
}

const (
	errorHandle = math.MaxUint64
	// TODO: handleMax needs to be configurable.
	// This value is arbitrary.
	handleMax = 2048
)

var (
	errInvalidHandle = errors.New("handle not found")
	errFull          = errors.New("all slots filled")
)

func newFileTable() *fileTable { return &fileTable{files: make([]*fileHandle, 0)} }

func (ft *fileTable) add(f fs.File) (fileDescriptor, error) {
	ft.Lock()
	defer ft.Unlock()
	files := ft.files
	for i, handle := range files {
		if handle == nil {
			files[i] = &fileHandle{goFile: f}
			return fileDescriptor(i), nil
		}
	}
	const initialSize = 8 // TODO: [review] Bigger? Smaller?
	newCap := generic.Max(cap(files)*2, initialSize)
	if newCap > handleMax {
		return errorHandle, errFull
	}
	var (
		newTable = make([]*fileHandle, len(files)+1, newCap)
		index    = (copy(newTable, files))
	)
	newTable[index] = &fileHandle{goFile: f}
	ft.files = newTable
	return fileDescriptor(index), nil
}

func (ft *fileTable) validLocked(fh fileDescriptor) error {
	var (
		files           = ft.files
		descriptorCount = len(files)
	)
	if descriptorCount == 0 {
		return errInvalidHandle
	}
	highestDescriptor := fileDescriptor(descriptorCount - 1)
	if highestDescriptor < fh {
		return errInvalidHandle
	}
	if files[fh] == nil {
		return errInvalidHandle
	}
	return nil
}

func (ft *fileTable) get(fh fileDescriptor) (*fileHandle, error) {
	ft.RLock()
	defer ft.RUnlock()
	if err := ft.validLocked(fh); err != nil {
		return nil, err
	}
	return ft.files[fh], nil
}

func (ft *fileTable) remove(fh fileDescriptor) error {
	ft.Lock()
	defer ft.Unlock()
	if err := ft.validLocked(fh); err != nil {
		return err
	}
	ft.files[fh] = nil
	// TODO: We could trim the slice here so that it's not wasting memory.
	// Need metrics on this though. May not be worth the cost.
	// And not sure what capacity we should trim to as a maximum.
	// If it's too low we're going to constantly thrash.
	// Too high and we'll be wasting memory.
	return nil
}

// TODO: [review] [Ame].
// This function must be scrutinized heavily and should be tested well.
// Currently neither are done.
// Closing the file,
// removing it's handle from the table,
// and preserving all errors (both POSIX's and Go's)
// are a must. Otherwise we'll retain dead references in the table,
// and neither the operator nor operating system will know what happened.
// (^Very bad if this happens.)
func (ft *fileTable) release(fh fileDescriptor) (errorCode errNo, err error) {
	file, err := ft.get(fh)
	if err != nil {
		return -fuse.EBADF, err
	}
	// NOTE: SUSv4;BSi7 `close` (paraphrased)
	// "If errors are encountered, the result of the handle is unspecified."
	// We'll remove the handle regardless of what the file's [Close] method does.
	defer func() {
		// User's [Close] method could panic.
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
			errorCode = -fuse.EIO
		}
		if rErr := ft.remove(fh); rErr != nil {
			// We already checked the validity of fh
			// and passed in [get]. So if this branch is hit,
			// it implies the file table implementation is broken
			// or memory was corrupted somehow.
			//
			// Don't overwrite error code if one was encountered.
			if errorCode == operationSuccess {
				errorCode = -fuse.EBADF
			}
			err = fserrors.Join(err, rErr)
		}
	}()
	if err := file.goFile.Close(); err != nil {
		return -fuse.EIO, err
	}
	return operationSuccess, nil
}

func (ft *fileTable) Close() error {
	ft.Lock()
	defer ft.Unlock()
	var err error
	for _, f := range ft.files {
		if cErr := f.goFile.Close(); cErr != nil {
			if err == nil {
				err = fmt.Errorf("failed to close: %w", cErr)
			} else {
				err = fmt.Errorf("%w - %s", err, cErr)
			}
		}
	}
	return err
}
