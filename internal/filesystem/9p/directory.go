package p9

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	perrors "github.com/djdv/p9/errors"
	"github.com/djdv/p9/fsimpl/templatefs"
	"github.com/djdv/p9/p9"
)

const parentWName = ".."

type (
	// Embeddable alias with a more apt name.
	directory = p9.File
	Directory struct {
		templatefs.NoopFile
		*fileTableSync
		*metadata
		*linkSync
		opened,
		cleanupElements bool
	}
	ephemeralDir struct {
		directory
		refs *atomic.Uintptr
		unlinkOnClose,
		unlinking *atomic.Bool
		closed bool
	}
	directorySettings struct {
		fileSettings
		cleanupSelf,
		cleanupElements bool
	}
	DirectoryOption        func(*directorySettings) error
	directorySetter[T any] interface {
		*T
		setCleanupSelf(bool)
		setCleanupElements(bool)
	}
)

func (ds *directorySettings) setCleanupSelf(b bool)     { ds.cleanupSelf = b }
func (ds *directorySettings) setCleanupElements(b bool) { ds.cleanupElements = b }

// UnlinkWhenEmpty causes files to unlink from their parent
// after they are considered empty and the last reference
// held by a Walk has been closed.
func UnlinkWhenEmpty[
	OT optionFunc[T],
	T any,
	I directorySetter[T],
](b bool,
) OT {
	return func(settings *T) error {
		any(settings).(I).setCleanupSelf(b)
		return nil
	}
}

// UnlinkEmptyChildren sets [UnlinkWhenEmpty]
// on files created by this file.
func UnlinkEmptyChildren[
	OT optionFunc[T],
	T any,
	I directorySetter[T],
](b bool,
) OT {
	return func(settings *T) error {
		any(settings).(I).setCleanupElements(b)
		return nil
	}
}

func NewDirectory(options ...DirectoryOption) (p9.QID, p9.File, error) {
	var settings directorySettings
	settings.metadata.initialize(p9.ModeDirectory)
	if err := applyOptions(&settings, options...); err != nil {
		return p9.QID{}, nil, err
	}
	return newDirectory(&settings)
}

func newDirectory(settings *directorySettings) (p9.QID, p9.File, error) {
	var file p9.File = &Directory{
		fileTableSync: newFileTable(),
		metadata:      &settings.metadata,
		linkSync:      &settings.linkSync,
	}
	if settings.cleanupSelf {
		if parent := settings.linkSync.parent; parent == nil {
			err := generic.ConstError("cannot unlink self without parent file")
			return p9.QID{}, nil, err
		}
		file = &ephemeralDir{
			directory:     file,
			refs:          new(atomic.Uintptr),
			unlinkOnClose: new(atomic.Bool),
			unlinking:     new(atomic.Bool),
		}
	}
	settings.metadata.fillDefaults()
	settings.metadata.incrementPath()
	return settings.QID, file, nil
}

func (dir *Directory) Walk(names []string) ([]p9.QID, p9.File, error) {
	if dir.opened {
		return nil, nil, fidOpenedErr
	}
	if len(names) == 0 {
		return nil, &Directory{
			fileTableSync:   dir.fileTableSync,
			metadata:        dir.metadata,
			linkSync:        dir.linkSync,
			cleanupElements: dir.cleanupElements,
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
		nwNames      = len(names)
		qids         = make([]p9.QID, 1, nwNames)
		attrMaskNone p9.AttrMask
	)
	if qids[0], _, _, err = clone.GetAttr(attrMaskNone); err != nil {
		return nil, nil, errors.Join(err, clone.Close())
	}
	if noRemainder := nwNames == 1; noRemainder {
		return qids, clone, nil
	}
	subQIDS, descendant, err := clone.Walk(names[1:])
	if err != nil {
		return nil, nil, errors.Join(err, clone.Close())
	}
	if err := clone.Close(); err != nil {
		return nil, nil, errors.Join(err, descendant.Close())
	}
	return append(qids, subQIDS...), descendant, nil
}

func (dir *Directory) backtrack(names []string) ([]p9.QID, p9.File, error) {
	var (
		qids   = make([]p9.QID, 1, len(names)+1)
		parent = dir.parent
	)
	if dirIsRoot := parent == nil; dirIsRoot {
		parent = dir
	}
	_, clone, err := parent.Walk(nil)
	if err != nil {
		return nil, nil, err
	}
	var attrMaskNone p9.AttrMask
	if qids[0], _, _, err = clone.GetAttr(attrMaskNone); err != nil {
		return nil, nil, errors.Join(err, clone.Close())
	}
	if noRemainder := len(names) == 0; noRemainder {
		return qids, clone, nil
	}
	// These could be ancestors, siblings, cousins, etc.
	// depending on the remaining names.
	relQIDS, relative, err := clone.Walk(names)
	if err != nil {
		return nil, nil, errors.Join(err, clone.Close())
	}
	if err := clone.Close(); err != nil {
		return nil, nil, errors.Join(err, relative.Close())
	}
	return append(qids, relQIDS...), relative, nil
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
	return dir.QID, noIOUnit, nil
}

func (dir *Directory) Link(file p9.File, name string) error {
	if !dir.exclusiveStore(name, file) {
		return perrors.EEXIST // TODO: spec; evalue
	}
	return nil
}

func (dir *Directory) UnlinkAt(name string, _ uint32) error {
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
	qid, newDir, err := NewDirectory(
		WithPath[DirectoryOption](dir.ninePath),
		WithPermissions[DirectoryOption](permissions),
		WithUID[DirectoryOption](uid),
		WithGID[DirectoryOption](gid),
		WithParent[DirectoryOption](dir, name),
		UnlinkWhenEmpty[DirectoryOption](dir.cleanupElements),
		UnlinkEmptyChildren[DirectoryOption](dir.cleanupElements),
		WithoutRename[DirectoryOption](dir.linkSync.renameDisabled),
	)
	if err == nil {
		err = dir.Link(newDir, name)
	}
	return qid, err
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

func (ed *ephemeralDir) Attach() (p9.File, error) { return ed, nil }

func (ed *ephemeralDir) Walk(names []string) ([]p9.QID, p9.File, error) {
	qids, file, err := ed.directory.Walk(names)
	if len(names) == 0 {
		refs := ed.refs
		refs.Add(1)
		file = &ephemeralDir{
			directory:     file,
			refs:          refs,
			unlinkOnClose: ed.unlinkOnClose,
			unlinking:     ed.unlinking,
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
		!ed.unlinkOnClose.Load() ||
		ed.unlinking.Load() {
		return nil
	}
	ed.unlinking.Store(true)
	return ed.unlinkSelf()
}

func (ed *ephemeralDir) Link(file p9.File, name string) error {
	dir := ed.directory.(*Directory)
	if err := dir.Link(file, name); err != nil {
		return err
	}
	ed.unlinkOnClose.Store(false)
	return nil
}

func (ed *ephemeralDir) UnlinkAt(name string, _ uint32) error {
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
		dir  = ed.directory.(*Directory)
		link = dir.linkSync
	)
	return unlinkChildSync(link)
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
