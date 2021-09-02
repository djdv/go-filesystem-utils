//go:build !nofuse
// +build !nofuse

package cgofuse

import (
	"errors"
	"fmt"
	"io/fs"
	"sync"
)

// TODO: review whole file; quickly ported

type (
	handle = uint64
	fMap   map[handle]fs.File
)

var (
	errInvalidHandle = errors.New("handle not found")
	errFull          = errors.New("all slots filled")
)

func newFileTable() *fileTableStruct { return &fileTableStruct{files: make(fMap)} }

type (
	fileTable interface {
		Add(fs.File) (handle, error)
		Exists(handle) bool
		Get(handle) (fs.File, error)
		Remove(handle) error
		Length() int
		Close() error
	}
	fileTableStruct struct {
		sync.RWMutex
		index   uint64
		wrapped bool // if true; we start reclaiming dead index values
		files   fMap
	}
)

func (ft *fileTableStruct) Add(f fs.File) (handle, error) {
	ft.Lock()
	defer ft.Unlock()

	ft.index++
	if !ft.wrapped && ft.index == handleMax {
		ft.wrapped = true
	}

	if ft.wrapped { // switch from increment mode to "search for free slot" mode
		for index := handle(0); index != handleMax; index++ {
			if _, ok := ft.files[index]; ok {
				// handle is in use
				continue
			}
			// a free handle was found; use it
			ft.index = index
			return index, nil
		}
		return errorHandle, errFull
	}

	// we've never hit the cap, so we can assume the handle is free
	// but for sanity we check anyway
	if _, ok := ft.files[ft.index]; ok {
		panic("handle should be uninitialized but is in use")
	}
	ft.files[ft.index] = f
	return ft.index, nil
}

func (ft *fileTableStruct) Exists(fh handle) bool {
	ft.RLock()
	defer ft.RUnlock()
	_, exists := ft.files[fh]
	return exists
}

func (ft *fileTableStruct) Get(fh handle) (fs.File, error) {
	ft.RLock()
	defer ft.RUnlock()
	f, exists := ft.files[fh]
	if !exists {
		return nil, errInvalidHandle
	}
	return f, nil
}

func (ft *fileTableStruct) Remove(fh handle) error {
	ft.Lock()
	defer ft.Unlock()
	if _, exists := ft.files[fh]; !exists {
		return errInvalidHandle
	}
	delete(ft.files, fh)
	return nil
}

func (ft *fileTableStruct) Length() int {
	ft.RLock()
	defer ft.RUnlock()
	return len(ft.files)
}

func (ft *fileTableStruct) Close() error {
	ft.Lock()
	defer ft.Unlock()
	var err error
	for _, f := range ft.files {
		if cErr := f.Close(); cErr != nil {
			if err == nil {
				err = fmt.Errorf("failed to close: %w", cErr)
			} else {
				err = fmt.Errorf("%w - %s", err, cErr)
			}
		}
	}
	return err
}
