package p9

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

const parentWName = ".."

type (
	// Embeddable alias with a more apt name.
	directory = p9.File
	Directory struct {
		templatefs.NoopFile
		*fileTableSync
		metadata
		*linkSync
		opened bool
	}
	// ephemeralDir will unlink from its parent,
	// on its final FID [Close].
	// But only after a call to [UnlinkAt]
	// has been performed on the last entry.
	// I.e. empty directories are allowed once,
	// for sequences like this:
	// `mkdir ed;cd ed;>file;rm file;cd ..` (ed is unlinked)
	// But also this:
	// `mkdir ed;cd ed;>file;rm file;>file2;cd ..` (ed is not unlinked)
	ephemeralDir struct {
		directory
		refs          *atomic.Uintptr
		unlinkOnClose *atomic.Bool
		closed        bool
	}
)

func NewDirectory(options ...DirectoryOption) (p9.QID, *Directory) {
	settings := directorySettings{
		metadata: makeMetadata(p9.ModeDirectory),
	}
	if err := parseOptions(&settings, options...); err != nil {
		panic(err)
	}
	settings.QID.Path = settings.ninePath.Add(1)
	return *settings.QID, &Directory{
		fileTableSync: newFileTable(),
		metadata:      settings.metadata,
		linkSync: &linkSync{
			link: settings.linkSettings,
		},
	}
}

func newDirectory(ephemeral bool, options ...DirectoryOption) (p9.QID, directory) {
	if ephemeral {
		return newEphemeralDirectory(options...)
	}
	return NewDirectory(options...)
}

func newEphemeralDirectory(options ...DirectoryOption) (p9.QID, *ephemeralDir) {
	qid, fsys := NewDirectory(options...)
	if parent := fsys.parent; parent == nil {
		panic("parent file missing, dir unlinkable") // TODO: better message
	}
	return qid, &ephemeralDir{
		directory:     fsys,
		refs:          new(atomic.Uintptr),
		unlinkOnClose: new(atomic.Bool),
	}
}

func (dir *Directory) Attach() (p9.File, error) { return dir, nil }

func (dir *Directory) Walk(names []string) ([]p9.QID, p9.File, error) {
	if dir.opened {
		return nil, nil, fidOpenedErr
	}
	if len(names) == 0 {
		return nil, &Directory{
			fileTableSync: dir.fileTableSync,
			metadata:      dir.metadata,
			linkSync:      dir.linkSync,
		}, nil
	}
	name := names[0]
	if name == parentWName {
		return dir.backtrack(names[1:])
	}
	child, ok := dir.load(name)
	if !ok {
		return nil, nil, perrors.ENOENT
	}
	_, clone, err := child.Walk(nil)
	if err != nil {
		return nil, nil, err
	}
	var (
		nwNames = len(names)
		qids    = make([]p9.QID, 1, nwNames)
	)
	if qids[0], _, _, err = clone.GetAttr(attrMaskNone); err != nil {
		return nil, nil, fserrors.Join(err, clone.Close())
	}
	if noRemainder := nwNames == 1; noRemainder {
		return qids, clone, nil
	}
	subQIDS, descendant, err := clone.Walk(names[1:])
	if err != nil {
		return nil, nil, err
	}
	return append(qids, subQIDS...), descendant, nil
}

func (dir *Directory) backtrack(names []string) ([]p9.QID, p9.File, error) {
	var (
		qids   = make([]p9.QID, 1, len(names)+1)
		parent = dir.parent
	)
	if areRoot := parent == nil; areRoot {
		parent = dir
		qids[0] = *dir.QID
	} else {
		var err error
		if qids[0], _, _, err = parent.GetAttr(attrMaskNone); err != nil {
			return nil, nil, err
		}
	}
	if noRemainder := len(names) == 0; noRemainder {
		return qids, parent, nil
	}
	return parent.Walk(names)
}

func (dir Directory) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	return dir.metadata.SetAttr(valid, attr)
}

func (dir Directory) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return dir.metadata.GetAttr(req)
}

func (dir *Directory) Open(mode p9.OpenFlags) (p9.QID, ioUnit, error) {
	if dir.opened {
		// TODO: [spec] correct evalue?
		return p9.QID{}, noIOUnit, perrors.EBADF
	}
	if mode.Mode() != p9.ReadOnly {
		return p9.QID{}, noIOUnit, perrors.EINVAL
	}
	dir.opened = true
	return *dir.QID, noIOUnit, nil
}

func (dir *Directory) Link(file p9.File, name string) error {
	if !dir.exclusiveStore(name, file) {
		return perrors.EEXIST // TODO: spec; evalue
	}
	return nil
}

func (dir *Directory) UnlinkAt(name string, flags uint32) error {
	if !dir.delete(name) {
		return perrors.ENOENT // TODO: spec; evalue
	}
	return nil
}

func (dir *Directory) Mkdir(name string, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, error) {
	uid, gid, err := mkPreamble(dir, name, uid, gid)
	if err != nil {
		return p9.QID{}, err
	}
	qid, newDir := NewDirectory(
		WithPath[DirectoryOption](dir.ninePath),
		WithPermissions[DirectoryOption](permissions),
		WithUID[DirectoryOption](uid),
		WithGID[DirectoryOption](gid),
		WithParent[DirectoryOption](dir, name),
	)
	return qid, dir.Link(newDir, name)
}

func (dir *Directory) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	return dir.to9Ents(offset, count)
}

func (dir *Directory) Rename(newDir p9.File, newName string) error {
	return dir.linkSync.rename(dir, newDir, newName)
}

func (dir *Directory) RenameAt(oldName string, newDir p9.File, newName string) error {
	return dir.linkSync.renameAt(dir, newDir, oldName, newName)
}

func (dir *Directory) Renamed(newDir p9.File, newName string) {
	dir.linkSync.Renamed(newDir, newName)
}

func (ed *ephemeralDir) Walk(names []string) ([]p9.QID, p9.File, error) {
	qids, file, err := ed.directory.Walk(names)
	if len(names) == 0 {
		refs := ed.refs
		refs.Add(1)
		file = &ephemeralDir{
			directory:     file,
			refs:          refs,
			unlinkOnClose: ed.unlinkOnClose,
		}
	}
	return qids, file, err
}

func (ed *ephemeralDir) Close() error {
	if ed.closed {
		return perrors.EBADF
	}
	ed.closed = true
	const decriment = ^uintptr(0)
	if active := ed.refs.Add(decriment); active != 0 ||
		!ed.unlinkOnClose.Load() {
		return nil
	}
	return ed.unlinkSelf()
}

func (ed *ephemeralDir) Mkdir(name string, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, error) {
	uid, gid, err := mkPreamble(ed, name, uid, gid)
	if err != nil {
		return p9.QID{}, err
	}
	qid, newDir := newEphemeralDirectory(
		WithPath[DirectoryOption](ed.directory.(*Directory).ninePath),
		WithPermissions[DirectoryOption](permissions),
		WithUID[DirectoryOption](uid),
		WithGID[DirectoryOption](gid),
		WithParent[DirectoryOption](ed, name),
	)
	return qid, ed.Link(newDir, name)
}

func (ed *ephemeralDir) Link(file p9.File, name string) error {
	dir := ed.directory.(*Directory)
	if err := dir.Link(file, name); err != nil {
		return err
	}
	ed.unlinkOnClose.Store(false)
	return nil
}

func (ed *ephemeralDir) UnlinkAt(name string, flags uint32) error {
	var (
		dir   = ed.directory.(*Directory)
		table = dir.fileTableSync
	)
	table.mu.Lock()
	defer table.mu.Unlock()
	if !table.deleteLocked(name) {
		return perrors.ENOENT // TODO: spec; evalue
	}
	if table.lengthLocked() == 0 {
		ed.unlinkOnClose.Store(true)
	}
	return nil
}

func (ed *ephemeralDir) unlinkSelf() error {
	var (
		dir    = ed.directory.(*Directory)
		link   = dir.link
		parent = link.parent
		self   = link.child
	)
	const flags = 0
	return parent.UnlinkAt(self, flags)
}

// XXX: arity. See if we can do something about this.
func mkSubdir(parent p9.File, path ninePath,
	name string, permissions p9.FileMode,
	uid p9.UID, gid p9.GID,
	ephemeral bool,
) (p9.QID, p9.File, error) {
	uid, gid, err := mkPreamble(parent,
		name, uid, gid)
	if err != nil {
		return p9.QID{}, nil, err
	}
	var (
		dirOpts = []DirectoryOption{
			WithPath[DirectoryOption](path),
			WithPermissions[DirectoryOption](permissions),
			WithUID[DirectoryOption](uid),
			WithGID[DirectoryOption](gid),
			WithParent[DirectoryOption](parent, name),
		}
		subQID, subDir = newDirectory(ephemeral, dirOpts...)
	)
	return subQID, subDir, nil
}

func childExists(fsys p9.File, name string) (bool, error) {
	_, file, err := fsys.Walk([]string{name})
	if err == nil {
		if err = file.Close(); err != nil {
			err = fmt.Errorf("could not close child: %w", err)
		}
		return true, err
	}
	if errors.Is(err, perrors.ENOENT) {
		err = nil
	}
	return false, err
}

// If any passed in IDs are invalid,
// they will be subsisted with values from fsys.
func maybeInheritIDs(fsys p9.File, uid p9.UID, gid p9.GID) (p9.UID, p9.GID, error) {
	var (
		getUID = !uid.Ok()
		getGID = !gid.Ok()
	)
	if getAttrs := getUID || getGID; !getAttrs {
		return uid, gid, nil
	}
	want := p9.AttrMask{
		UID: getUID,
		GID: getGID,
	}
	_, _, attr, err := fsys.GetAttr(want)
	if err != nil {
		return p9.NoUID, p9.NoGID, err
	}
	if getUID {
		uid = attr.UID
	}
	if getGID {
		gid = attr.GID
	}
	return uid, gid, nil
}
