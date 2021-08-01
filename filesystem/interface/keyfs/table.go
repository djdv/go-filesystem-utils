package keyfs

import (
	"sync"
	"sync/atomic"

	"github.com/ipfs/go-ipfs/filesystem"
)

type closer func() error      // io.Closure closure wrapper
func (f closer) Close() error { return f() }

type refcount func(increment bool) error

func (f refcount) increment()       { f(true) }
func (f refcount) decrement() error { return f(false) }

type refCounter interface {
	increment()
	decrement() error // error value must only be populated when reference count reaches 0
}

// XXX: trash garbage, don't imitate
// we need to utilize a mutex here because the lock is shared across all references, not unique per instance
// so we can't just use a merged uintptr{MSB:count;LSB:lock} value
// i.e. the counter is atomic but the cleanup can't be
func newRefCounter(locker sync.Locker, onZero func() error) refcount {
	var count uintptr = 1

	return func(increment bool) error {
		if increment {
			atomic.AddUintptr(&count, 1)
			return nil
		}

		locker.Lock()
		defer locker.Unlock()

		if atomic.AddUintptr(&count, ^uintptr(0)) == 0 {
			// TODO: references should be timebombed
			// it's very typical for the APIs like FUSE to make a sequence of single requests
			// which means a sequence of create;reap;create;reap...
			// references should stay alive and in the table for a few ms at least after the final decrement
			// and a ticker here should be canceled on increment if one is called before the ticker is done
			return onZero()
		}
		return nil
	}
}

type (
	openFileFunc      func() (filesystem.File, error)
	openInterfaceFunc func() (filesystem.Interface, error)

	referenceTable interface {
		// retrieves and existing reference and returns it, or opens a new one using provided function
		getFileRef(string, openFileFunc) (fileRef, error)
		getRootRef(string, openInterfaceFunc) (rootRef, error)
	}

	fileTable struct {
		sync.Mutex
		refs map[string]fileRef
	}

	rootTable struct {
		sync.Mutex
		refs map[string]rootRef
	}

	combinedTable struct {
		fileTable
		rootTable
	}
)

func newReferenceTable() referenceTable {
	return &combinedTable{
		fileTable: fileTable{refs: make(map[string]fileRef)},
		rootTable: rootTable{refs: make(map[string]rootRef)},
	}
}

func (ft *fileTable) getFileRef(keyName string, opener openFileFunc) (fileRef, error) {
	ft.Lock()
	defer ft.Unlock()

	// if a `File` reference already exists, use it
	ref, ok := ft.refs[keyName]
	if ok {
		ref.counter.increment()
		return ref, nil
	}

	// otherwise open a new one and set it up …
	file, err := opener()
	if err != nil {
		return fileRef{}, err
	}

	// … so that it removes itself from the table when its counter reaches 0 …
	whenZeroRefs := func() error { // ft will be locked during this
		delete(ft.refs, keyName)
		return file.Close()
	}

	// … and decrements its counter on `Close`
	fileRef := fileRef{
		File:    file,
		Mutex:   new(sync.Mutex),
		counter: newRefCounter(&ft.Mutex, whenZeroRefs),
	}
	fileRef.Closer = (closer)(fileRef.counter.decrement) // self referential

	ft.refs[keyName] = fileRef
	return fileRef, nil
}

func (rt *rootTable) getRootRef(keyName string, opener openInterfaceFunc) (rootRef, error) {
	rt.Lock()
	defer rt.Unlock()

	// if an `Interface` reference already exists, use it
	ref, ok := rt.refs[keyName]
	if ok {
		ref.counter.increment()
		return ref, nil
	}

	// otherwise open a new one and set it up …
	root, err := opener()
	if err != nil {
		return rootRef{}, err
	}

	// … so that it removes itself from the table when its counter reaches 0
	whenZeroRefs := func() error { // rt will be locked during this
		delete(rt.refs, keyName)
		return root.Close()
	}

	// NOTE: the counter starts at 1 and is decremented on `rootRef.Close`
	rootRef := rootRef{
		Interface: root,
		counter:   newRefCounter(&rt.Mutex, whenZeroRefs),
	}

	rt.refs[keyName] = rootRef
	return rootRef, nil
}
