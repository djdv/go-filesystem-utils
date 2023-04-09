package p9

import (
	"sync"

	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
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
		mu       sync.Mutex
		disabled bool
	}
	linkOption func(*linkSync) error
)

// TODO:
// we need to figure out how best to handle this
func newLinkSync(options ...linkOption) (*linkSync, error) {
	var link linkSync
	if err := parseOptions(&link, options...); err != nil {
		return nil, err
	}
	return &link, nil
}

func WithParent[OT Options](parent p9.File, child string) (option OT) {
	return makeFieldFunc[OT]("link", func(lnk *link) error {
		*lnk = link{
			parent: parent,
			child:  child,
		}
		return nil
	})
}

// TODO: docs
// ephemeralDir will unlink from its parent,
// on its final FID [Close].
// But only after a call to [UnlinkAt]
// has been performed on the last entry.
// I.e. empty directories are allowed once,
// for sequences like this:
// `mkdir ed;cd ed;>file;rm file;cd ..` (ed is unlinked)
// But also this:
// `mkdir ed;cd ed;>file;rm file;>file2;cd ..` (ed is not unlinked)
func UnlinkWhenEmpty[OT DirectoryOptions](b bool) (option OT) {
	return makeFieldSetter[OT]("cleanupSelf", b)
}

// TODO: docs
// tells the file that files it creates
// should be unlinked when they become empty.
// essentially cascading UnlinkWhenEmpty.
func UnlinkEmptyChildren[OT DirectoryOptions](b bool) (option OT) {
	return makeFieldSetter[OT]("cleanupElements", b)
}

func WithoutRename[OT Options](disabled bool) OT {
	return makeFieldSetter[OT]("disabled", disabled)
}

// WithRenamedFunc provides a callback
// which is called after a successful rename operation.
func WithRenamedFunc[OT Options](fn RenamedFunc) OT {
	return makeFieldSetter[OT]("renamedFn", fn)
}

func (ls *linkSync) rename(file, newDir p9.File, newName string) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.disabled {
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
	if ls.disabled {
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
