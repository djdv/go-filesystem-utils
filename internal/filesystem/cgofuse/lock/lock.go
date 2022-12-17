// Package lock implements file system operation locks
// based around hierarchical `/` delimited paths.
package lock

import (
	"path"
	"strings"
	"sync"
)

type (
	pathLock struct {
		pathMu,
		dataMu sync.RWMutex
	}
	pathLockReference struct {
		pathLock
		referenceCount int
	}
	pathLockerMap map[string]*pathLockReference
	// PathLocker is a hierarchical path locker,
	// inspired by Ritik Malhotra's paper on path-based locks.
	PathLocker struct {
		lockTableMu sync.Mutex
		lockTable   pathLockerMap
	}
	// UnlockFunc must be called after an operation completes.
	// Typically a single defer statement is used
	// to acquire and later release a lock from a [PathLocker].
	//  defer locker.Operation(args...)()
	UnlockFunc = func()
	lockFunc   = func()
	// componentFunc is an abstraction to reduce duplication.
	// See: [makeSequenceLocked].
	componentFunc func(path, data *sync.RWMutex) (lockFunc, UnlockFunc)
)

// componentIndex returns indices that can be used
// to retrieve components of a slash delimited string.
//
// The initial delimiter and final component are included.
//
// E.g. path `"/a/b/c"` produces indices `[]int{1, 2, 4, 6}`
//
//	path[:indices[0]] == "/"
//	path[:indices[1]] == "/a"
//	path[:indices[2]] == "/a/b"
//	path[:indices[3]] == "/a/b/c"
func componentIndex(path string) []int {
	const (
		delimiterNotFound = -1
		delimiterByte     = '/'
		delimiterString   = string(delimiterByte)
		delimiterLength   = len(delimiterString)
	)
	if path == delimiterString {
		return []int{delimiterLength}
	}
	var (
		fullPath          = len(path)
		slashCount        = strings.Count(path, delimiterString)
		componentIndicies = make([]int, slashCount+1)
		cursor            int
	)
	for componentIndex := 0; ; componentIndex++ {
		delimiterIndex := strings.IndexByte(path, delimiterByte)
		if delimiterIndex == delimiterNotFound {
			componentIndicies[len(componentIndicies)-1] = fullPath
			return componentIndicies
		}
		offset := delimiterIndex + delimiterLength
		cursor += offset
		path = path[offset:]
		if componentIndex == 0 { // Special case to include the root delimiter.
			componentIndicies[componentIndex] = cursor
		} else {
			componentIndicies[componentIndex] = cursor - 1
		}
	}
}

// replaceName decaps oldpath,
// returning newname on top of it.
func replaceName(oldpath, newname string) string {
	const delimiter = '/'
	var (
		newPath strings.Builder
		parent  = path.Dir(oldpath)
	)
	newPath.Grow(len(parent) + 1 + len(newname))
	newPath.WriteString(parent)
	newPath.WriteRune(delimiter)
	newPath.WriteString(newname)
	return newPath.String()
}

func makeLockerPairs(size int) ([]lockFunc, []UnlockFunc) {
	return make([]lockFunc, size), make([]UnlockFunc, size)
}

func lockAll(lockers []lockFunc) {
	for _, lockFn := range lockers {
		lockFn()
	}
}

func genUnlockInReverseOrder(ml *PathLocker, unlockers []UnlockFunc) UnlockFunc {
	return func() {
		ml.lockTableMu.Lock()
		defer ml.lockTableMu.Unlock()
		for i := len(unlockers) - 1; i != -1; i-- {
			unlockers[i]()
		}
	}
}

func (lm pathLockerMap) upsert(path string) *pathLockReference {
	if lock, ok := lm[path]; ok {
		lock.referenceCount++
		return lock
	}
	lock := &pathLockReference{referenceCount: 1}
	lm[path] = lock
	return lock
}

// genRefCleanupWrapper wraps an [UnlockFunc]
// decrementing `lock`'s refcount in addition to calling [unlockFn].
// [pathLockerMap] must be guarded before calling the returned [UnlockFunc]
// as it will be modified by the last reference.
func (lm pathLockerMap) genRefCleanupWrapper(lock *pathLockReference,
	component string, unlockFn UnlockFunc,
) UnlockFunc {
	return func() {
		if lock.referenceCount--; lock.referenceCount == 0 {
			// NOTE: [lm] must be locked by caller to guard this [delete].
			// See: [genUnlockInReverseOrder] which holds the lock
			// before calling any unlockers.
			delete(lm, component)
		}
		unlockFn()
	}
}

// genDualWriteLock returns functions which
// target both path and data locks for the component.
func (lm pathLockerMap) genDualWriteLock(lock *pathLockReference, component string) (lockFunc, UnlockFunc) {
	return func() { lock.pathMu.Lock(); lock.dataMu.Lock() },
		lm.genRefCleanupWrapper(lock, component,
			func() { lock.dataMu.Unlock(); lock.pathMu.Unlock() })
}

func (ml *PathLocker) getLockMapLocked() pathLockerMap {
	if locks := ml.lockTable; locks != nil {
		return locks
	}
	locks := make(pathLockerMap)
	ml.lockTable = locks
	return locks
}

// lockAndGenUnlocker locks its table before calling [makeSequenceLocked].
// It then initiates the lock sequence, before returning an unlock sequence
// (wrapped as a single [UnlockFunc]).
func (ml *PathLocker) lockAndGenUnlocker(path string, sequenceFn componentFunc) UnlockFunc {
	ml.lockTableMu.Lock()
	lockers, unlockers := ml.makeSequenceLocked(path, sequenceFn)
	ml.lockTableMu.Unlock()
	lockAll(lockers)
	return genUnlockInReverseOrder(ml, unlockers)
}

// makeSequenceLocked generates a sequence of read-locks
// for all path components, up to the last component.
// componentFn is called with lock references for the last component.
func (ml *PathLocker) makeSequenceLocked(path string, componentFn componentFunc) ([]lockFunc, []UnlockFunc) {
	var (
		lockIndex          int
		locks              = ml.getLockMapLocked()
		componentIndex     = componentIndex(path)
		lockCount          = len(componentIndex)
		lockers, unlockers = makeLockerPairs(lockCount)
	)
	for _, pathIndex := range componentIndex[:len(componentIndex)-1] {
		var (
			component = path[:pathIndex]
			lock      = locks.upsert(component)
		)
		lockers[lockIndex] = lock.pathMu.RLock
		unlockers[lockIndex] = locks.genRefCleanupWrapper(lock, component, lock.pathMu.RUnlock)
		lockIndex++
	}
	var (
		component        = path[:componentIndex[len(componentIndex)-1]]
		lock             = locks.upsert(component)
		lockFn, unlockFn = componentFn(
			&lock.pathMu,
			&lock.dataMu,
		)
	)
	lockers[lockIndex] = lockFn
	unlockers[lockIndex] = locks.genRefCleanupWrapper(lock, component, unlockFn)
	return lockers, unlockers
}

// CreateOrDelete should be used when an object is to be created or deleted at/from `path`.
func (ml *PathLocker) CreateOrDelete(path string) UnlockFunc {
	return ml.lockAndGenUnlocker(path,
		func(path, data *sync.RWMutex) (lockFunc, UnlockFunc) {
			return func() { path.Lock(); data.Lock() },
				func() { data.Unlock(); path.Unlock() }
		})
}

// Access should be used when an object's data or metadata is to be read.
func (ml *PathLocker) Access(path string) UnlockFunc {
	return ml.lockAndGenUnlocker(path,
		func(path, data *sync.RWMutex) (lockFunc, UnlockFunc) {
			return func() { path.RLock(); data.RLock() },
				func() { data.RUnlock(); path.RUnlock() }
		})
}

// Modify should be used when an object's data or metadata is to be written to.
func (ml *PathLocker) Modify(path string) UnlockFunc {
	return ml.lockAndGenUnlocker(path,
		func(path, data *sync.RWMutex) (lockFunc, UnlockFunc) {
			return func() { path.RLock(); data.Lock() },
				func() { data.Unlock(); path.RUnlock() }
		})
}

// Rename should be used when 'oldpath' is to be renamed
// within its parent directory.
func (ml *PathLocker) Rename(oldpath, newname string) UnlockFunc {
	ml.lockTableMu.Lock()
	var (
		parentLockers, parentUnlockers = ml.makeParentLocksLocked(oldpath)
		locks                          = ml.getLockMapLocked()
		newPath                        = replaceName(oldpath, newname)
		oldLock                        = locks.upsert(oldpath)
		newLock                        = locks.upsert(newPath)
	)
	ml.lockTableMu.Unlock()
	var (
		oldLocks, oldUnlocks = locks.genDualWriteLock(oldLock, oldpath)
		newLocks, newUnlocks = locks.genDualWriteLock(newLock, newPath)
		lockers              = append(parentLockers, oldLocks, newLocks)
		unlockers            = append(parentUnlockers, oldUnlocks, newUnlocks)
	)
	lockAll(lockers)
	return genUnlockInReverseOrder(ml, unlockers)
}

// Move should be used when `oldpath` is to be moved
// (and optionally renamed) to a new directory.
func (ml *PathLocker) Move(oldpath, newpath string) UnlockFunc {
	ml.lockTableMu.Lock()
	var (
		oldParentLockers, oldParentUnlockers = ml.makeParentLocksLocked(oldpath)
		newParentLockers, newParentUnlockers = ml.makeParentLocksLocked(newpath)
		locks                                = ml.getLockMapLocked()
		oldLock                              = locks.upsert(oldpath)
		newLock                              = locks.upsert(newpath)
	)
	ml.lockTableMu.Unlock()
	var (
		oldLocks, oldUnlocks = locks.genDualWriteLock(oldLock, oldpath)
		newLocks, newUnlocks = locks.genDualWriteLock(newLock, newpath)
		lockers              = append(append(oldParentLockers, newParentLockers...),
			oldLocks, newLocks)
		unlockers = append(append(oldParentUnlockers, newParentUnlockers...),
			oldUnlocks, newUnlocks)
	)
	lockAll(lockers)
	return genUnlockInReverseOrder(ml, unlockers)
}

func (ml *PathLocker) makeParentLocksLocked(name string) ([]lockFunc, []UnlockFunc) {
	return ml.makeSequenceLocked(
		path.Dir(name),
		func(path, data *sync.RWMutex) (lockFunc, UnlockFunc) {
			return func() { path.RLock(); data.RLock() },
				func() { data.RUnlock(); path.RUnlock() }
		})
}
