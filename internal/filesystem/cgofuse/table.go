package cgofuse

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"sync"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/winfsp/cgofuse/fuse"
)

type (
	handleSlice []*fileHandle
	fileTable   struct {
		files handleSlice
		sync.RWMutex
	}
)

const (
	errorHandle = math.MaxUint64
	// TODO: handleMax needs to be configurable like `ulimit` allows.
	// NOTE: file table sizes and bounds were chosen arbitrarily.
	// Suggestions for better averages or ways to tune are welcome.
	handleMax              = 4096 // TODO: POSIX calls this "OPEN_MAX", we could in camelCase.
	tableStartingSize      = 8
	tableGrowthfactor      = 2
	tableShrinkLimitFactor = tableGrowthfactor * 2
	tableShrinkBound       = tableStartingSize * tableShrinkLimitFactor

	errInvalidHandle = generic.ConstError("handle not found")
	errFull          = generic.ConstError("all slots filled")
)

func newFileTable() *fileTable { return new(fileTable) }

func (files handleSlice) lowestAvailable() fileDescriptor {
	for i, handle := range files {
		if handle == nil {
			return fileDescriptor(i)
		}
	}
	return errorHandle
}

func (files handleSlice) extend() (handleSlice, error) {
	var (
		filesLen = len(files)
		filesEnd = filesLen + 1
		filesCap = cap(files)
	)
	if filesLen < filesCap {
		return files[:filesEnd], nil
	}
	var (
		scaledCap = filesCap * tableGrowthfactor
		newCap    = generic.Max(scaledCap, tableStartingSize)
	)
	if newCap > handleMax {
		return nil, errFull
	}
	newTable := make([]*fileHandle, filesEnd, newCap)
	copy(newTable, files)
	return newTable, nil
}

func (files handleSlice) shrink(lowerBound int) handleSlice {
	var (
		emptySlots int
		filesLen   = len(files)
	)
	for i := filesLen - 1; i >= 0; i-- {
		if files[i] != nil {
			break
		}
		emptySlots++
	}
	var (
		newLen             = filesLen - emptySlots
		newCap             = lowestAlignment(newLen, tableStartingSize)
		tooSmall           = newCap < lowerBound
		sameSize           = newCap == cap(files)
		lessOrEqualToBound = tooSmall || sameSize
	)
	if lessOrEqualToBound {
		return nil
	}
	newTable := make(handleSlice, newLen, newCap)
	copy(newTable, files)
	return newTable
}

func lowestAlignment(size, alignment int) int {
	remainder := (size - 1) % alignment
	return (size - 1) + (alignment - remainder)
}

func (ft *fileTable) add(f fs.File) (fileDescriptor, error) {
	ft.Lock()
	defer ft.Unlock()
	files := ft.files
	if index := files.lowestAvailable(); index != errorHandle {
		files[index] = &fileHandle{goFile: f}
		return index, nil
	}
	newTable, err := files.extend()
	if err != nil {
		return errorHandle, err
	}

	index := len(newTable) - 1
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
	files := ft.files
	files[fh] = nil
	if cap(files) > tableShrinkBound {
		if newTable := files.shrink(tableShrinkBound); newTable != nil {
			ft.files = newTable
		}
	}
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
			err = errors.Join(err, rErr)
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
	var errs []error
	for i, handle := range ft.files {
		if handle == nil {
			// `nil` handles may be present in the file table.
			// Lower handle indices may be removed before
			// higher ones, but the table will maintain its
			// length.
			// The table may also be at its minimum length
			// with no open handles at all.
			continue
		}
		if err := handle.goFile.Close(); err != nil {
			errs = append(
				errs,
				fmt.Errorf(
					"failed to close handle %d: %w",
					i, err,
				),
			)
		}
	}
	return errors.Join(errs...)
}
