package files

import (
	"log"
	"sync/atomic"

	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

type (
	Directory struct {
		templatefs.NoopFile
		fileTable
		metadata
		opened bool
	}
	ephemeralDir struct {
		p9.File
		parent p9.File
		path   *atomic.Uint64
		name   string
	}
)

func NewDirectory(options ...MetaOption) (p9.QID, *Directory) {
	meta := makeMetadata(p9.ModeDirectory, options...)
	return *meta.QID, &Directory{
		metadata:  meta,
		fileTable: newFileTable(),
	}
}

// func newLinkedFile(parent, file p9.File) p9.File { return linkedFile{parent: parent, File: file} }
func newEphemeralDir(parent p9.File, name string, options ...MetaOption) (p9.QID, *ephemeralDir) {
	qid, dir := NewDirectory(options...)
	return qid, &ephemeralDir{
		File:   dir,
		parent: parent,
		path:   dir.path,
		name:   name,
	}
}

func (dir *ephemeralDir) files() fileTable {
	// XXX: Magic; We need to change something to eliminate this.
	return dir.File.(interface {
		files() fileTable
	}).files()
}

func (dir *ephemeralDir) clone(withQID bool) ([]p9.QID, *ephemeralDir, error) {
	var wnames []string
	if withQID {
		wnames = []string{selfWName}
	}
	qids, dirClone, err := dir.File.Walk(wnames)
	if err != nil {
		return nil, nil, err
	}
	newDir := &ephemeralDir{
		File:   dirClone,
		parent: dir.parent,
		path:   dir.path,
		name:   dir.name,
	}
	return qids, newDir, nil
}

func (dir *Directory) Attach() (p9.File, error) { return dir, nil }

func (dir *Directory) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	return dir.metadata.SetAttr(valid, attr)
}

func (dir *Directory) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return dir.metadata.GetAttr(req)
}

func (dir *Directory) Walk(names []string) ([]p9.QID, p9.File, error) {
	return walk[*Directory](dir, names...)
}

func (dir *Directory) Link(file p9.File, name string) error {
	// log.Printf("dir, linking (%T) from (%T): %s", file, dir, name)
	if !dir.exclusiveStore(name, file) {
		return perrors.EEXIST // TODO: spec; evalue
	}
	return nil
}

func (dir *Directory) UnlinkAt(name string, flags uint32) error {
	log.Printf("d(%T) - unlink: %s", dir, name)
	if !dir.fileTable.delete(name) {
		return perrors.ENOENT // TODO: spec; evalue
	}
	return nil
}

func (dir *Directory) Mkdir(name string, permissions p9.FileMode, _ p9.UID, gid p9.GID) (p9.QID, error) {
	if _, exists := dir.load(name); exists {
		return p9.QID{}, perrors.EEXIST
	}
	qid, newDir := NewDirectory(WithPath(dir.path))
	if err := newDir.SetAttr(mkdirMask(permissions, dir.UID, gid)); err != nil {
		return *newDir.QID, err
	}
	return qid, dir.Link(newDir, name)
}

func (dir *Directory) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	if mode.Mode() != p9.ReadOnly {
		// TODO: [spec] correct evalue?
		return p9.QID{}, 0, perrors.EINVAL
	}
	if dir.opened {
		return p9.QID{}, 0, perrors.EBADF
	}
	dir.opened = true
	return *dir.QID, 0, nil
}

func (dir *Directory) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	return dir.fileTable.to9Ents(offset, count)
}

func (dir *Directory) fidOpened() bool  { return dir.opened }
func (dir *Directory) files() fileTable { return dir.fileTable }

func (dir *Directory) clone(withQID bool) ([]p9.QID, *Directory, error) {
	var (
		qids   []p9.QID
		newDir = &Directory{
			metadata:  dir.metadata,
			fileTable: dir.fileTable,
		}
	)
	if withQID {
		qids = []p9.QID{*newDir.QID}
	}
	return qids, newDir, nil
}

func (dir *ephemeralDir) UnlinkAt(name string, flags uint32) error {
	log.Printf("ed(%T) - unlink: %s", dir, name)
	if err := dir.File.UnlinkAt(name, flags); err != nil {
		return err
	}
	log.Printf("ed(%T) - unlink - post: %s", dir, name)
	ents, err := ReadDir(dir.File)
	if err != nil {
		return err
	}
	if len(ents) == 0 {
		log.Println("removing self:", dir.name)
		return dir.parent.UnlinkAt(dir.name, flags)
	} else {
		log.Println("contents still here for ", dir.name, " ", ents)
	}
	return nil
}
