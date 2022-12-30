package p9

import (
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

const errNotDirectory = generic.ConstError("type does not implement directory interface extensions")

type (
	directory interface {
		p9.File
		fileTable
		placeholderName
		// entry(name string) (p9.File, error)
		// TODO: can we eliminate this?
		path() ninePath
		//
	}
	Directory struct {
		fileTable
		File
	}
	ephemeralDir struct {
		*Directory
	}
)

func assertDirectory(dir p9.File) (directory, error) {
	typedDir, ok := dir.(directory)
	if !ok {
		err := fmt.Errorf("%T: %w", dir, errNotDirectory)
		return nil, err
	}
	return typedDir, nil
}

func NewDirectory(options ...DirectoryOption) (p9.QID, *Directory) {
	var settings directorySettings
	if err := parseOptions(&settings, options...); err != nil {
		panic(err)
	}
	var (
		metadata       = settings.metadata
		withTimestamps = settings.withTimestamps
	)
	initMetadata(&metadata, p9.ModeDirectory, withTimestamps)
	return *metadata.QID, &Directory{
		fileTable: newFileTable(),
		File: File{
			metadata: metadata,
			link:     settings.linkSettings,
		},
	}
}

func newEphemeralDirectory(options ...DirectoryOption) (_ p9.QID, directory *ephemeralDir) {
	qid, fsys := NewDirectory(options...)
	if parent := fsys.parent; parent == nil {
		panic("parent file missing, dir unlinkable") // TODO: better message
	}
	return qid, &ephemeralDir{Directory: fsys}
}

func (dir *Directory) Attach() (p9.File, error) { return dir, nil }

func (dir *Directory) clone(withQID bool) (qs []p9.QID, clone *Directory, _ error) {
	clone = &Directory{
		fileTable: dir.fileTable,
		File: File{
			metadata: dir.metadata,
			link:     dir.link,
		},
	}
	if withQID {
		qs = []p9.QID{*clone.QID}
	}
	return
}

func (dir *Directory) Walk(names []string) ([]p9.QID, p9.File, error) {
	return walk[*Directory](dir, names...)
}

func (dir *Directory) Open(mode p9.OpenFlags) (p9.QID, ioUnit, error) {
	if mode.Mode() != p9.ReadOnly {
		// TODO: [spec] correct evalue?
		return p9.QID{}, noIOUnit, perrors.EINVAL
	}
	if dir.fidOpened() {
		return p9.QID{}, noIOUnit, perrors.EBADF
	}
	dir.openFlag = true
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

func (dir *Directory) Mkdir(name string, permissions p9.FileMode, _ p9.UID, gid p9.GID) (p9.QID, error) {
	if _, exists := dir.load(name); exists {
		return p9.QID{}, perrors.EEXIST
	}
	directoryOptions := []DirectoryOption{
		WithSuboptions[DirectoryOption](
			WithPath(dir.ninePath),
			WithBaseAttr(&p9.Attr{
				Mode: mkdirMask(permissions),
				UID:  dir.UID,
				GID:  gid,
			}),
			WithAttrTimestamps(true),
		),
		WithSuboptions[DirectoryOption](
			WithParent(dir, name),
		),
	}
	qid, newDir := NewDirectory(directoryOptions...)
	return qid, dir.Link(newDir, name)
}

func (dir *Directory) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	return dir.to9Ents(offset, count)
}

func rename(oldDir, newDir, file p9.File, oldName, newName string) error {
	if err := newDir.Link(file, newName); err != nil {
		return err
	}
	const flags = 0
	err := oldDir.UnlinkAt(oldName, flags)
	if err == nil {
		return nil
	}
	return fserrors.Join(err, newDir.UnlinkAt(newName, flags))
}

func (dir *Directory) Rename(newDir p9.File, newName string) error {
	var (
		parent  = dir.link.parent
		oldName = dir.link.name
	)
	return rename(parent, newDir, dir, oldName, newName)
}

func (dir *Directory) RenameAt(oldName string, newDir p9.File, newName string) error {
	parent := dir.link.parent
	return rename(parent, newDir, dir, oldName, newName)
}

func (dir *Directory) Renamed(newDir p9.File, newName string) {
	dir.link.parent = newDir
	dir.link.name = newName
}

func (eDir *ephemeralDir) clone(withQID bool) ([]p9.QID, *ephemeralDir, error) {
	qids, dir, err := eDir.Directory.clone(withQID)
	if err != nil {
		return nil, nil, err
	}
	return qids, &ephemeralDir{Directory: dir}, nil
}

func (eDir *ephemeralDir) UnlinkAt(name string, flags uint32) error {
	if err := eDir.Directory.UnlinkAt(name, flags); err != nil {
		return err
	}
	ents, err := ReadDir(eDir.Directory)
	if err != nil {
		return err
	}
	if len(ents) == 0 {
		var (
			link   = eDir.link
			parent = link.parent
			self   = link.name
		)
		return parent.UnlinkAt(self, flags)
	}
	return nil
}
