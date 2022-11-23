package lock

import (
	"strings"
	"sync"
)

// TODO: [review] This implementation is both incomplete
// and likely suboptimal. Its current state is intended to be enough to be usable.
// But it should be made optimal eventually, since it's the hottest code-path in FUSE.
// (Every fuse operation must call these methods.)
//
// We also might want to move this pkg somewhere else.
// It's fine under `filesystem/cgofuse` for now, but might be applicable to other pkgs.
// We'd need to name things better to move it up to `/filesystem/something-lock`

type (
	pathLock struct {
		pathMu,
		dataMu sync.RWMutex
	}
	pathLockReference struct {
		pathLock
		// TODO: [review] Double check the logic.
		// I'm pretty sure this count doesn't have to be atomic;
		// when used within [pathLocker]. Since that has a global mutex.
		referenceCount int
	}
	pathLockerMap map[string]*pathLockReference
	// PathLocker is a hierarchical path locker,
	// inspired by Ritik Malhotra's paper on path-based locks.
	PathLocker struct {
		lockTableMu sync.Mutex
		lockTable   pathLockerMap
	}
	UnlockFunc = func()
	lockFunc   = func()
	// TODO: [Ame] English and names.
	// sequenceFunc is an abstraction to reduce duplication.
	// Intermediate components of a path, all take the same locks.
	// The final component will have a unique (un)lock sequence
	// that is dependant on the operation.
	// This function may use the provided locks to generate that sequence.
	sequenceFunc func(path, data *sync.RWMutex) (lockFunc, UnlockFunc)
)

func (lm pathLockerMap) upsert(path string) *pathLockReference {
	if lock, ok := lm[path]; ok {
		lock.referenceCount++
		return lock
	}
	lock := &pathLockReference{referenceCount: 1}
	lm[path] = lock
	return lock
}

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
	var (
		fullPath          = len(path)
		slashCount        = strings.Count(path, delimiterString)
		componentIndicies = make([]int, slashCount+1)
		cursor            int
	)
	for i := 0; ; i++ {
		delimiterIndex := strings.IndexByte(path, delimiterByte)
		if delimiterIndex == delimiterNotFound {
			componentIndicies[len(componentIndicies)-1] = fullPath
			return componentIndicies
		}
		offset := delimiterIndex + delimiterLength
		cursor += offset
		path = path[offset:]
		if i == 0 { // Special case to include the root delimiter.
			componentIndicies[i] = cursor
		} else {
			componentIndicies[i] = cursor - 1
		}
	}
}

func (ml *PathLocker) getLockMap() pathLockerMap {
	if locks := ml.lockTable; locks != nil {
		return locks
	}
	locks := make(pathLockerMap)
	ml.lockTable = locks
	return locks
}

func makeLockerPairs(size int) ([]lockFunc, []UnlockFunc) {
	return make([]lockFunc, size), make([]UnlockFunc, size)
}

func blockCaller(lockers []lockFunc) {
	for _, lockFn := range lockers {
		lockFn()
	}
}

func (ml *PathLocker) blockAndGenUnlocker(path string, sequenceFn sequenceFunc) UnlockFunc {
	ml.lockTableMu.Lock()
	lockers, unlockers := ml.makeSequence(path, sequenceFn)
	ml.lockTableMu.Unlock()
	blockCaller(lockers)
	return unlockerWithCleanup(ml, unlockers)
}

func unlockerWithCleanup(ml *PathLocker, unlockers []UnlockFunc) UnlockFunc {
	return func() {
		ml.lockTableMu.Lock()
		defer ml.lockTableMu.Unlock()
		for i := len(unlockers) - 1; i != -1; i-- {
			unlockers[i]()
		}
	}
}

func (ml *PathLocker) makeSequence(path string, sequenceFn sequenceFunc) ([]lockFunc, []UnlockFunc) {
	var (
		locks              = ml.getLockMap()
		componentIndex     = componentIndex(path)
		lastComponent      = len(componentIndex) - 1
		lockCount          = len(componentIndex)
		lockers, unlockers = makeLockerPairs(lockCount)
	)
	for i, pIndex := range componentIndex {
		var (
			lockFn    lockFunc
			unlockFn  UnlockFunc
			component = path[:pIndex]
			lock      = locks.upsert(component)
		)
		if i != lastComponent {
			lockFn = lock.pathMu.RLock
			unlockFn = lock.pathMu.RUnlock
		} else {
			lockFn, unlockFn = sequenceFn(
				&lock.pathMu,
				&lock.dataMu,
			)
		}
		lockers[i] = lockFn
		unlockers[i] = func() {
			if lock.referenceCount--; lock.referenceCount == 0 {
				delete(locks, component)
			}
			unlockFn()
		}
	}
	return lockers, unlockers
}

func (ml *PathLocker) CreateOrDelete(path string) UnlockFunc {
	return ml.blockAndGenUnlocker(path,
		func(path, data *sync.RWMutex) (lockFunc, UnlockFunc) {
			return func() { path.Lock(); data.Lock() },
				func() { data.Unlock(); path.Unlock() }
		})
}

func (ml *PathLocker) Access(path string) UnlockFunc {
	return ml.blockAndGenUnlocker(path,
		func(path, data *sync.RWMutex) (lockFunc, UnlockFunc) {
			return func() { path.RLock(); data.RLock() },
				func() { data.RUnlock(); path.RUnlock() }
		})
}

func (ml *PathLocker) Modify(path string) UnlockFunc {
	return ml.blockAndGenUnlocker(path,
		func(path, data *sync.RWMutex) (lockFunc, UnlockFunc) {
			return func() { path.RLock(); data.Lock() },
				func() { data.Unlock(); path.RUnlock() }
		})
}

// TODO: We need this for write calls when implemented.
func (ml *PathLocker) Rename(fusePath, newName string) UnlockFunc {
	panic("NIY")
}

// TODO: We need this for write calls when implemented.
func (ml *PathLocker) Move(fusePath, newPath string) UnlockFunc {
	panic("NIY")
}
