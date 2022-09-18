package files

import (
	"log"

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
	linkedFile struct {
		parent p9.File
		p9.File
	}
	ephemeralDir struct {
		parent p9.File
		*Directory
		name string
	}
)

func NewDirectory(options ...MetaOption) (p9.QID, *Directory) {
	meta := makeMetadata(p9.ModeDirectory, options...)
	return *meta.QID, &Directory{
		metadata:  meta,
		fileTable: newFileTable(),
	}
}

func newLinkedFile(parent, file p9.File) p9.File { return linkedFile{parent: parent, File: file} }

func newEphemeralDir(parent p9.File, name string, options ...MetaOption) (p9.QID, *ephemeralDir) {
	qid, dir := NewDirectory(options...)
	return qid, &ephemeralDir{
		name:      name,
		parent:    parent,
		Directory: dir,
	}
}

func (dir *ephemeralDir) clone(withQID bool) ([]p9.QID, *ephemeralDir, error) {
	var (
		qids   []p9.QID
		newDir = &ephemeralDir{
			name:      dir.name,
			parent:    dir.parent,
			Directory: dir.Directory,
		}
	)
	if withQID {
		qids = []p9.QID{*newDir.QID}
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
	log.Printf("dir, linking (%T) from (%T): %s", file, dir, name)
	if !dir.exclusiveStore(name, newLinkedFile(dir, file)) {
		return perrors.EEXIST // TODO: spec; evalue
	}
	return nil
}

func (dir *Directory) UnlinkAt(name string, flags uint32) error {
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

func (dir *Directory) fidOpened() bool { return dir.opened }

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

func (lf linkedFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	parent := lf.parent
	switch nameCount := len(names); nameCount {
	case 0:
		_, file, err := lf.File.Walk(nil)
		return nil, &linkedFile{
			parent: parent,
			File:   file,
		}, err
	case 1:
		switch names[0] {
		case parentWName:
			return parent.Walk([]string{selfWName})
		case selfWName:
			qids, file, err := lf.File.Walk([]string{selfWName})
			return qids, &linkedFile{
				parent: parent,
				File:   file,
			}, err
		}
	}
	return lf.File.Walk(names)
}

func (dir *ephemeralDir) UnlinkAt(name string, flags uint32) error {
	if !dir.fileTable.delete(name) {
		return perrors.ENOENT // TODO: spec; evalue
	}
	if dir.fileTable.length() == 0 {
		return dir.parent.UnlinkAt(dir.name, flags)
	}
	return nil
}

func (dir *ephemeralDir) Link(file p9.File, name string) error {
	log.Println("T1")
	if !dir.exclusiveStore(name, newLinkedFile(dir, file)) {
		return perrors.EEXIST
	}
	return nil
}

/*
func (dir linkedFile) Link(file p9.File, name string) error {
	log.Println("T2")
	return perrors.ENOSYS
}
*/
