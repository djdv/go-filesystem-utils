package files

import (
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

type (
	Directory struct {
		File
		fileTable
	}
	ephemeralDir struct {
		*Directory
	}
)

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

func (dir *Directory) clone(withQID bool) ([]p9.QID, *Directory, error) {
	var (
		qids   []p9.QID
		newDir = &Directory{
			fileTable: dir.fileTable,
			File: File{
				metadata: dir.metadata,
				link:     dir.link,
			},
		}
	)
	if withQID {
		qids = []p9.QID{*newDir.QID}
	}
	return qids, newDir, nil
}

func (dir *Directory) Walk(names []string) ([]p9.QID, p9.File, error) {
	return walk[*Directory](dir, names...)
}

func (dir *Directory) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	if mode.Mode() != p9.ReadOnly {
		// TODO: [spec] correct evalue?
		return p9.QID{}, 0, perrors.EINVAL
	}
	if dir.fidOpened() {
		return p9.QID{}, 0, perrors.EBADF
	}
	dir.openFlag = true
	return *dir.QID, 0, nil
}

func (dir *Directory) files() fileTable { return dir.fileTable }

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
			WithPath(dir.path),
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
	if oldDir == nil || oldName == "" {
		return perrors.ENOENT // TODO: [spec] check if this is the right evalue to use
	}
	// TODO: attempt rollback on error
	if err := newDir.Link(file, newName); err != nil {
		return err
	}
	const flags = 0
	return oldDir.UnlinkAt(oldName, flags)
}

func (dir *Directory) Rename(newDir p9.File, newName string) error {
	var (
		parent  = dir.parent
		oldName = dir.name
	)
	return rename(parent, newDir, dir, oldName, newName)
}

func (dir *Directory) RenameAt(oldName string, newDir p9.File, newName string) error {
	parent := dir.parent
	return rename(parent, newDir, dir, oldName, newName)
}

func (eDir *ephemeralDir) clone(withQID bool) ([]p9.QID, *ephemeralDir, error) {
	qids, dir, err := eDir.Directory.clone(withQID)
	if err != nil {
		return nil, nil, err
	}
	newDir := &ephemeralDir{
		Directory: dir,
	}
	return qids, newDir, nil
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
		return eDir.parent.UnlinkAt(eDir.name, flags)
	}
	return nil
}
