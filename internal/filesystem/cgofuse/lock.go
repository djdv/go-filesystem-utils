//go:build !nofuse

package cgofuse

import (
	"strings"
	"sync"
)

type (
	// TODO: these need to be exported
	lockFn   func()
	unlockFn func()

	fileSystemLock struct {
		path,
		data sync.RWMutex
	}

	// TODO: we don't actually do any reference counting or deletes yet
	// the lock map can grow infinitely large right now.
	// unlock fn needs to atomically decrement and acquire the global write lock.
	fileSystemLockRef struct {
		fileSystemLock
		ref int
	}

	fsLockMap map[string]*fileSystemLockRef

	// operationsLock is inspired by
	// Ritik Malhotra's paper on path-based locks.
	//
	// TODO: consider changing the interface to take in the []int index
	// instead of generating it.
	// This way we can do it once on Open, store it on the handle,
	// and use it for subsequent ops on the same file.
	operationsLock interface {
		CreateOrDelete(fusePath string) unlockFn
		Access(fusePath string) unlockFn
		Modify(fusePath string) unlockFn
		Rename(fusePath, newName string) unlockFn
		Move(fusePath, newPath string) unlockFn
	}

	// mapPathLocker is an implementation
	// of a hierarchical path locker.
	mapPathLocker struct {
		global sync.Mutex
		locks  fsLockMap
	}
)

// returns a list of indices used to iterate over components
// in a slash delimited path string; via `path[:index]`.
// Includes initial delimiter, and final component.
// e.g. "/a/b/c" => [1 2 3 6]
// path[:index] == "/"
// path[:index] == "/a"
// path[:index] == "/a/"
// path[:index] == "/a/b/c"
func pathIndex(path string) []int {
	// TODO: [reivew] There may be a more optimal way to do this.
	// This has to be fast because it's in the hottest path.
	// We're trying to avoid allocating slices of individual copies of components
	// But we allocate an arbitrary (growing) slice of ints anyway 🤔
	const (
		delim      = '/'
		indexAlloc = 8 // NOTE: Arbitrary; change if it makes sense to.
		// Should be some average path depth.
	)
	var (
		fullPath       = len(path)
		componentIndex = make([]int, 0, indexAlloc)
		lastIndex      int
	)
	for i := 0; ; i++ {
		slashIndex := strings.IndexRune(path, delim)
		if slashIndex == -1 {
			// Final index is always the full string.
			componentIndex = append(componentIndex, fullPath)
			break
		}
		if i == 0 {
			// Include the initial delimiter.
			lastIndex += slashIndex + 1
		} else {
			// Include up to the next delimiter.
			lastIndex += slashIndex
		}
		componentIndex = append(componentIndex, lastIndex)

		path = path[slashIndex+1:] // <<Left shift at the delineation.
	}

	return componentIndex
}

func newOperationsLock() operationsLock { return new(mapPathLocker) }

// TODO: dedupe; we should be able to reduce these down to a single function
// that takes in a switch
// E.g. lock(intermediates, target struct{path{r|w}, data{r|w})
// just, something that informs it which locks to take for which components.

func (ml *mapPathLocker) CreateOrDelete(fusePath string) unlockFn {
	ml.global.Lock()
	var (
		locks          = ml.locks
		componentIndex = pathIndex(fusePath)
		lastComponent  = len(componentIndex) - 1
		lockCount      = len(componentIndex)
		lockers        = make([]lockFn, lockCount)
		unlockers      = make([]unlockFn, lockCount)
	)
	if locks == nil {
		locks = make(fsLockMap)
		ml.locks = locks
	}

	for i, pIndex := range componentIndex {
		var (
			lockFn           lockFn
			unlockFn         unlockFn
			path             = fusePath[:pIndex]
			lock, lockExists = locks[path]
		)
		if !lockExists {
			lock = &fileSystemLockRef{ref: 1}
			locks[path] = lock
		} else {
			lock.ref++
		}

		// Intermediates.
		if i != lastComponent {
			lockFn = lock.path.RLock
			unlockFn = lock.path.RUnlock
		} else { // Target.
			lockFn = func() { lock.path.Lock(); lock.data.Lock() }
			unlockFn = func() { lock.data.Unlock(); lock.path.Unlock() }
		}

		lockers[i] = lockFn
		unlockers[i] = func() {
			lock.ref--
			if lock.ref == 0 {
				delete(locks, path)
			}
			unlockFn()
		}
	}

	ml.global.Unlock()

	// Block the caller until they can do their operation.
	for _, lockFn := range lockers {
		lockFn()
	}

	return unlockerWithCleanup(ml, unlockers)
}

func unlockerWithCleanup(ml *mapPathLocker, unlockers []unlockFn) unlockFn {
	return func() {
		// We might manipulate the lock table if a lock's refcount is 0.
		ml.global.Lock()
		defer ml.global.Unlock()
		for i := len(unlockers) - 1; i != -1; i-- {
			unlockers[i]()
		}
	}
}

func (ml *mapPathLocker) Access(fusePath string) unlockFn {
	ml.global.Lock()
	var (
		locks          = ml.locks
		componentIndex = pathIndex(fusePath)
		lastComponent  = len(componentIndex) - 1
		lockCount      = len(componentIndex)
		lockers        = make([]lockFn, lockCount)
		unlockers      = make([]unlockFn, lockCount)
	)
	if locks == nil {
		locks = make(fsLockMap)
		ml.locks = locks
	}

	for i, pIndex := range componentIndex {
		var (
			lockFn           lockFn
			unlockFn         unlockFn
			path             = fusePath[:pIndex]
			lock, lockExists = locks[path]
		)
		if !lockExists {
			lock = &fileSystemLockRef{ref: 1}
			locks[path] = lock
		} else {
			lock.ref++
		}

		// Intermediates.
		if i != lastComponent {
			lockFn = lock.path.RLock
			unlockFn = lock.path.RUnlock
		} else { // Target.
			lockFn = func() { lock.path.RLock(); lock.data.RLock() }
			unlockFn = func() { lock.data.RUnlock(); lock.path.RUnlock() }
		}

		lockers[i] = lockFn
		unlockers[i] = func() {
			lock.ref--
			if lock.ref == 0 {
				delete(locks, path)
			}
			unlockFn()
		}
	}

	ml.global.Unlock()

	// Block the caller until they can do their operation.
	for _, lockFn := range lockers {
		lockFn()
	}

	return unlockerWithCleanup(ml, unlockers)
}

func (ml *mapPathLocker) Modify(fusePath string) unlockFn {
	ml.global.Lock()
	var (
		locks          = ml.locks
		componentIndex = pathIndex(fusePath)
		lastComponent  = len(componentIndex) - 1
		lockCount      = len(componentIndex)
		lockers        = make([]lockFn, lockCount)
		unlockers      = make([]unlockFn, lockCount)
	)
	if locks == nil {
		locks = make(fsLockMap)
		ml.locks = locks
	}

	for i, pIndex := range componentIndex {
		var (
			lockFn           lockFn
			unlockFn         unlockFn
			path             = fusePath[:pIndex]
			lock, lockExists = locks[path]
		)
		if !lockExists {
			lock = &fileSystemLockRef{ref: 1}
			locks[path] = lock
		} else {
			lock.ref++
		}

		// Intermediates.
		if i != lastComponent {
			lockFn = lock.path.RLock
			unlockFn = lock.path.RUnlock
		} else { // Target.
			lockFn = func() { lock.path.RLock(); lock.data.Lock() }
			unlockFn = func() { lock.data.Unlock(); lock.path.RUnlock() }
		}

		lockers[i] = lockFn
		unlockers[i] = func() {
			lock.ref--
			if lock.ref == 0 {
				delete(locks, path)
			}
			unlockFn()
		}
	}

	ml.global.Unlock()

	// Block the caller until they can do their operation.
	for _, lockFn := range lockers {
		lockFn()
	}

	return unlockerWithCleanup(ml, unlockers)
}

// TODO: We need this for write calls when implemented.
func (ml *mapPathLocker) Rename(fusePath, newName string) unlockFn {
	panic("NIY")
}

// TODO: We need this for write calls when implemented.
func (ml *mapPathLocker) Move(fusePath, newPath string) unlockFn {
	panic("NIY")
}
