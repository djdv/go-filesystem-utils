package files

import (
	"sync/atomic"

	"github.com/hugelgupf/p9/p9"
)

// TODO: some way to provide statfs for files that are themselves,
// not devices, but hosted inside one.
//
// Implementations should probably have a default of `0x01021997` (V9FS_MAGIC) for `f_type`
// Or we can make up our own magic numbers (something not already in use)
// to guarantee we're not misinterpreted (as a FS that we're not)
// by callers / the OS (Linux specifically).
//
// The Linux manual has this to say about `f_fsid`
// "Nobody knows what f_fsid is supposed to contain" ...
// we'll uhhh... figure something out later I guess.

type (
	DirectoryOption func(*Directory) error
	CloserOption    func(*Closer) error

	// NOTE: [go/issues/48522] is on the milestone for 1.20,
	// but was punted from 1.19 so maybe|maybe-not.
	// If it happens, implementers of this can be simplified dramatically.
	// For now lots of type boilerplate is needed,
	// and it's going to be easy to forget to add types to all shared opts. :^(
	//
	// TODO better name
	// We can unexport this too but it's kind of rude and ugly for the decls that need it.
	SharedOptions interface {
		DirectoryOption | CloserOption
	}
)

func WithPath[OT SharedOptions](path *atomic.Uint64) (option OT) {
	switch fnPtrPtr := any(&option).(type) {
	case *DirectoryOption:
		*fnPtrPtr = func(dir *Directory) error { dir.path = path; return nil }
	case *CloserOption:
		*fnPtrPtr = func(cl *Closer) error { cl.path = path; return nil }
	}
	return option
}

// TODO: this option may be removed or changed
// We may want to take in and apply a mask.
// It may make more sense to have OrMode instead that can be called multiple times
// with multiple values.
//
// TODO: document that this is literally permissions, not mode.Set,
// higher bits are filtered explicitly.
func WithPermissions[OT SharedOptions](permissions p9.FileMode) (option OT) {
	switch fnPtrPtr := any(&option).(type) {
	case *DirectoryOption:
		*fnPtrPtr = func(dir *Directory) error {
			dir.Mode |= permissions.Permissions()
			return nil
		}
	case *CloserOption:
		*fnPtrPtr = func(cl *Closer) error {
			cl.Mode |= permissions.Permissions()
			return nil
		}
	}
	return option
}

func WithParent[OT SharedOptions](parent p9.File) (option OT) {
	switch fnPtrPtr := any(&option).(type) {
	case *DirectoryOption:
		*fnPtrPtr = func(dir *Directory) error { dir.parent = parent; return nil }
	case *CloserOption:
		*fnPtrPtr = func(cl *Closer) error { cl.parent = parent; return nil }
	}
	return option
}

func WithUID[OT SharedOptions](uid p9.UID) (option OT) {
	switch fnPtrPtr := any(&option).(type) {
	case *DirectoryOption:
		*fnPtrPtr = func(dir *Directory) error { dir.UID = uid; return nil }
	case *CloserOption:
		*fnPtrPtr = func(cl *Closer) error { cl.Attr.UID = uid; return nil }
	}
	return option
}

func WithGID[OT SharedOptions](gid p9.GID) (option OT) {
	switch fnPtrPtr := any(&option).(type) {
	case *DirectoryOption:
		*fnPtrPtr = func(dir *Directory) error { dir.GID = gid; return nil }
	case *CloserOption:
		*fnPtrPtr = func(cl *Closer) error { cl.Attr.GID = gid; return nil }
	}
	return option
}

func WithRDev[OT SharedOptions](rdev p9.Dev) (option OT) {
	switch fnPtrPtr := any(&option).(type) {
	case *DirectoryOption:
		*fnPtrPtr = func(dir *Directory) error { dir.Attr.RDev = rdev; return nil }
	case *CloserOption:
		*fnPtrPtr = func(cl *Closer) error { cl.Attr.RDev = rdev; return nil }
	}
	return option
}
