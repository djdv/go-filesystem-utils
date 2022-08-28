package files

import (
	"sync/atomic"
	"time"

	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

type (
	// TODO: rename to unexported mapDirectory?
	Directory struct {
		templatefs.NoopFile
		parentFile p9.File
		path       *atomic.Uint64
		entries    fileTable
		*p9.Attr
		*p9.QID
		opened bool
	}
)

func NewDirectory(options ...DirectoryOption) *Directory {
	var (
		qid, attr = newMeta(p9.TypeDir)
		dir       = &Directory{
			QID:     qid,
			Attr:    attr,
			entries: newFileTable(),
		}
	)
	for _, setFunc := range options {
		if err := setFunc(dir); err != nil {
			panic(err)
		}
	}
	setupOrUsePather(&dir.QID.Path, &dir.path)
	return dir
}

func (dir *Directory) Attach() (p9.File, error) { return dir, nil }

func (dir *Directory) fidOpened() bool  { return dir.opened }
func (dir *Directory) parent() p9.File  { return dir.parentFile }
func (dir *Directory) files() fileTable { return dir.entries }
func (dir *Directory) clone(withQID bool) ([]p9.QID, *Directory) {
	var (
		qids   []p9.QID
		newDir = &Directory{
			parentFile: dir.parentFile,
			path:       dir.path,
			QID:        dir.QID,
			Attr:       dir.Attr,
			entries:    dir.entries,
		}
	)
	if withQID {
		qids = []p9.QID{*newDir.QID}
	}
	return qids, newDir
}

func (dir *Directory) Walk(names []string) ([]p9.QID, p9.File, error) {
	return walk[*Directory](dir, names...)
}

func (dir *Directory) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	var (
		now             time.Time
		nowSec, nowNano uint64
		usingClock      = valid.ATime || valid.MTime || valid.CTime ||
			valid.ATimeNotSystemTime || valid.MTimeNotSystemTime
	)
	if usingClock {
		now = time.Now()
		nowSec = uint64(now.Second())
		nowNano = uint64(now.Nanosecond())
	}
	dir.Attr.Apply(valid, attr)
	if !valid.ATimeNotSystemTime && valid.ATime {
		dir.Attr.ATimeSeconds = nowSec
		dir.Attr.ATimeNanoSeconds = nowNano
	}
	if !valid.MTimeNotSystemTime && valid.MTime {
		dir.Attr.MTimeSeconds = nowSec
		dir.Attr.MTimeNanoSeconds = nowNano
	}
	if valid.CTime {
		if dir.Attr.CTimeNanoSeconds != 0 {
			return perrors.EINVAL // TODO: eValue; this may only be set once for now - spec unclear.
		}
	}
	return nil
}

func (dir *Directory) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	var (
		qid          = *dir.QID
		filled, attr = fillAttrs(req, dir.Attr)
	)
	return qid, filled, *attr, nil
}

func (dir *Directory) Link(file p9.File, name string) error {
	// TODO: incomplete impl; for testing
	if !dir.entries.exclusiveStore(name, file) {
		return perrors.EEXIST // TODO: spec; evalue
	}
	return nil
}

func (dir *Directory) UnlinkAt(name string, flags uint32) error {
	// TODO: incomplete impl; for testing
	if !dir.entries.delete(name) {
		return perrors.ENOENT // TODO: spec; evalue
	}
	return nil
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

func (dir *Directory) Mkdir(name string, permissions p9.FileMode, _ p9.UID, gid p9.GID) (p9.QID, error) {
	newDir := NewDirectory(
		WithParent[DirectoryOption](dir),
		WithPath[DirectoryOption](dir.path),
	)
	if err := newDir.SetAttr(mkdirMask(permissions, dir.UID, gid)); err != nil {
		return *newDir.QID, err
	}
	return *newDir.QID, dir.Link(newDir, name)
}

func (dir *Directory) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	return dir.entries.to9Ents(offset, count)
}
