//+build !nofuse

package cgofuse

import (
	"errors"
	"sync"

	"github.com/ipfs/go-ipfs/filesystem"
)

type (
	handle = uint64
	fMap   map[handle]filesystem.File
	dMap   map[handle]filesystem.Directory
)

var (
	errInvalidHandle = errors.New("handle not found")
	errFull          = errors.New("all slots filled")
)

func newFileTable() *fileTableStruct           { return &fileTableStruct{files: make(fMap)} }
func newDirectoryTable() *directoryTableStruct { return &directoryTableStruct{directories: make(dMap)} }

type (
	fileTable interface {
		Add(filesystem.File) (handle, error)
		Exists(handle) bool
		Get(handle) (filesystem.File, error)
		Remove(handle) error
		Length() int
		// TODO: [lint]
		// List() []string // This might be nice to have; list names of handles, but not necessary
	}
	fileTableStruct struct {
		sync.RWMutex
		index   uint64
		wrapped bool // if true; we start reclaiming dead index values
		files   fMap
	}
)

func (ft *fileTableStruct) Add(f filesystem.File) (handle, error) {
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

func (ft *fileTableStruct) Get(fh handle) (filesystem.File, error) {
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

type (
	directoryTable interface {
		Add(filesystem.Directory) (handle, error)
		Exists(handle) bool
		Get(handle) (filesystem.Directory, error)
		Remove(handle) error
		Length() int
	}
	directoryTableStruct struct {
		sync.RWMutex
		index       uint64
		wrapped     bool // if true; we start reclaiming dead index values
		directories map[handle]filesystem.Directory
	}
)

func (dt *directoryTableStruct) Add(f filesystem.Directory) (handle, error) {
	dt.Lock()
	defer dt.Unlock()

	dt.index++
	if !dt.wrapped && dt.index == handleMax {
		dt.wrapped = true
	}

	if dt.wrapped { // switch from increment mode to "search for free slot" mode
		for index := handle(0); index != handleMax; index++ {
			if _, ok := dt.directories[index]; ok {
				// handle is in use
				continue
			}
			// a free handle was found; use it
			dt.index = index
			return index, nil
		}
		return errorHandle, errFull
	}

	// we've never hit the cap, so we can assume the handle is free
	// but for sanity we check anyway
	if _, ok := dt.directories[dt.index]; ok {
		panic("handle should be uninitialized but is in use")
	}
	dt.directories[dt.index] = f
	return dt.index, nil
}

func (dt *directoryTableStruct) Exists(fh handle) bool {
	dt.RLock()
	defer dt.RUnlock()
	_, exists := dt.directories[fh]
	return exists
}

func (dt *directoryTableStruct) Get(fh handle) (filesystem.Directory, error) {
	dt.RLock()
	defer dt.RUnlock()
	f, exists := dt.directories[fh]
	if !exists {
		return nil, errInvalidHandle
	}
	return f, nil
}

func (dt *directoryTableStruct) Remove(fh handle) error {
	dt.Lock()
	defer dt.Unlock()
	if _, exists := dt.directories[fh]; !exists {
		return errInvalidHandle
	}
	delete(dt.directories, fh)
	return nil
}

func (dt *directoryTableStruct) Length() int {
	dt.RLock()
	defer dt.RUnlock()
	return len(dt.directories)
}
