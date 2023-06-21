package p9

import (
	"sync"

	perrors "github.com/djdv/p9/errors"
	"github.com/djdv/p9/p9"
)

type (
	link struct {
		parent p9.File
		child  string
	}
	RenamedFunc func(old, new string)
	linkSync    struct {
		renamedFn RenamedFunc
		link
		mu             sync.Mutex
		renameDisabled bool
	}
	linkSetter[T any] interface {
		*T
		setParent(p9.File, string)
		disableRename(bool)
		setRenamedFunc(RenamedFunc)
	}
)

func (ls *linkSync) setParent(parent p9.File, child string) { ls.parent = parent; ls.child = child }
func (ls *linkSync) disableRename(disabled bool)            { ls.renameDisabled = disabled }
func (ls *linkSync) setRenamedFunc(fn RenamedFunc)          { ls.renamedFn = fn }

func WithParent[
	OT optionFunc[T],
	T any,
	I linkSetter[T],
](parent p9.File, child string,
) OT {
	return func(link *T) error {
		any(link).(I).setParent(parent, child)
		return nil
	}
}

// WithoutRename causes rename operations
// to return an error when called.
func WithoutRename[
	OT optionFunc[T],
	T any,
	I linkSetter[T],
](disabled bool,
) OT {
	return func(link *T) error {
		any(link).(I).disableRename(disabled)
		return nil
	}
}

// WithRenamedFunc provides a callback
// which is called after a successful rename operation.
func WithRenamedFunc[
	OT optionFunc[T],
	T any,
	I linkSetter[T],
](fn RenamedFunc,
) OT {
	return func(link *T) error {
		any(link).(I).setRenamedFunc(fn)
		return nil
	}
}

func (ls *linkSync) rename(file, newDir p9.File, newName string) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.renameDisabled {
		return perrors.EACCES
	}
	parent := ls.parent
	if parent == nil {
		// We allow this for now, but ENOENT
		// would also make sense here (on POSIX).
		return newDir.Link(file, newName)
	}
	return rename(file, parent, newDir, ls.child, newName)
}

func (ls *linkSync) renameAt(oldDir, newDir p9.File, oldName, newName string) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.renameDisabled {
		return perrors.EACCES
	}
	return renameAt(oldDir, newDir, oldName, newName)
}

func (ls *linkSync) Renamed(newDir p9.File, newName string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if renamed := ls.renamedFn; renamed != nil {
		defer renamed(ls.link.child, newName)
	}
	ls.link.parent = newDir
	ls.link.child = newName
}
