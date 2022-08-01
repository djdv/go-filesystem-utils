package files

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

const (
	shutdownName = "shutdown"
)

type (
	fileTable struct {
		mu     sync.RWMutex
		_table map[string]p9.File
	}

	Directory struct {
		templatefs.NoopFile
		parent  p9.File
		path    *atomic.Uint64
		entries *fileTable
		p9.Attr
		p9.QID
		opened bool
	}
)

// TODO: we probably don't need all these methods.

func (ft *fileTable) store(filename string, file p9.File) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft._table[filename] = file
}

func (ft *fileTable) upsert(filename string, file p9.File) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft._table[filename] = file
}

func (ft *fileTable) exclusiveStore(filename string, file p9.File) bool {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if _, ok := ft._table[filename]; ok {
		return false
	}
	ft._table[filename] = file
	return true
}

func (ft *fileTable) load(filename string) (p9.File, bool) {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	file, ok := ft._table[filename]
	return file, ok
}

func (ft *fileTable) length() int {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return len(ft._table)
}

func (ft *fileTable) flatten(offset uint64, count uint32) ([]string, []p9.File) {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	var (
		i       int
		entries = ft._table
		names   = make([]string, len(entries))
	)
	for name := range entries {
		names[i] = name
		i++
	}
	sort.Strings(names)
	names = names[offset : offset+uint64(min(len(names), int(count)))]

	files := make([]p9.File, len(names))
	for i, name := range names {
		files[i] = entries[name]
	}
	return names, files
}

func (ft *fileTable) to9Ents(offset uint64, count uint32) (p9.Dirents, error) {
	// TODO: This is (currently) safe but that might not be true forever.
	// We shouldn't acquire the read lock recursively.
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	if entCount := ft.length(); offset >= uint64(entCount) {
		return nil, nil
	}
	var (
		names, files = ft.flatten(offset, count)
		ents         = make(p9.Dirents, len(names))
	)
	for i, name := range names {
		q, _, _, err := files[i].GetAttr(p9.AttrMask{})
		if err != nil {
			return nil, err
		}
		ents[i] = p9.Dirent{
			QID:    q,
			Offset: offset + uint64(i) + 1,
			Type:   q.Type,
			Name:   name,
		}
	}
	return ents, nil
}

func (ft *fileTable) delete(filename string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	delete(ft._table, filename)
}

// TODO: we'll probably need to return QID
func NewDirectory(options ...DirectoryOption) *Directory {
	dir := &Directory{
		QID: p9.QID{Type: p9.TypeDir},
		Attr: p9.Attr{
			Mode: p9.ModeDirectory,
			UID:  p9.NoUID,
			GID:  p9.NoGID,
		},
		entries: &fileTable{_table: make(map[string]p9.File)},
	}
	for _, setFunc := range options {
		if err := setFunc(dir); err != nil {
			panic(err)
		}
	}
	setupOrUsePather(&dir.QID.Path, &dir.path)

	return dir
}

// TODO: Remove this method; [45ecbfb2-430b-48e0-847d-a6f78eac7816]
func (dir *Directory) Path() *atomic.Uint64 { return dir.path }

func (dir *Directory) Attach() (p9.File, error) { return dir, nil }

func (dir *Directory) clone(with cloneQid) ([]p9.QID, *Directory) {
	// TODO: direct copies are probably best
	// but consider using the constructor with options if it seems nicer.
	var (
		qids   []p9.QID
		newDir = &Directory{
			parent:  dir.parent,
			path:    dir.path,
			QID:     dir.QID,
			Attr:    dir.Attr,
			entries: dir.entries,
		}
	)
	if with {
		qids = []p9.QID{newDir.QID}
	}
	return qids, newDir
}

func (dir *Directory) Walk(names []string) ([]p9.QID, p9.File, error) {
	if dir.opened {
		return nil, nil, perrors.EINVAL // TODO: [spec] correct evalue?
	}
	switch wnames := len(names); wnames {
	case 0:
		_, nf := dir.clone(withoutQid)
		return nil, nf, nil
	case 1:
		switch names[0] {
		case parentWName:
			if parent := dir.parent; parent != nil {
				qid, _, _, err := dir.parent.GetAttr(p9.AttrMask{})
				return []p9.QID{qid}, parent, err
			}
			fallthrough
		case selfWName:
			qids, nf := dir.clone(withQid)
			return qids, nf, nil
		}
	}

	var (
		files    = dir.entries
		file, ok = files.load(names[0])
		qids     = make([]p9.QID, 0, len(names))
	)
	if !ok {
		return nil, nil, perrors.ENOENT
	}
	qid, _, _, err := file.GetAttr(p9.AttrMask{})
	if err != nil {
		return nil, nil, err
	}
	qids = append(qids, qid)

	// Hoist this into the switch.
	// Default case should have a recursive walk.
	if len(names) == 1 {
		return qids, file, nil
	}
	return nil, nil, fmt.Errorf("not implemented yet")
	// TODO: recur + append-return results
	// subQids, end, err := file.Walk(names[1:])
}

func (dir *Directory) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	var (
		qid          = dir.QID
		filled, attr = fillAttrs(req, &dir.Attr)
	)
	return qid, filled, *attr, nil
}

func (dir *Directory) Link(file p9.File, name string) error {
	// TODO: incomplete impl; for testing
	// needs to check existence first, and other things.
	if !dir.entries.exclusiveStore(name, file) {
		return perrors.EEXIST // TODO: spec; evalue
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
	return dir.QID, 0, nil
}

func (dir *Directory) Mkdir(name string, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, error) {
	// TODO: permissions audit
	// this is from open(5) but 9 create is not L mkdir, so check L manual and use their mask if different.
	// permissions &= (^p9.AllPermissions | dir.Mode.Permissions()&p9.AllPermissions)
	newDir := NewDirectory(
		WithParent[DirectoryOption](dir),
		WithPath[DirectoryOption](dir.path),
		WithPermissions[DirectoryOption](permissions),
		WithUID[DirectoryOption](uid),
		WithGID[DirectoryOption](gid),
	)

	if !dir.entries.exclusiveStore(name, newDir) {
		return p9.QID{}, perrors.EEXIST // TODO: spec; evalue
	}
	return newDir.QID, nil
}

func (dir *Directory) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	return dir.entries.to9Ents(offset, count)
}

/*
func setupRootDevices(root *Directory) error {
		for _, pair := range []struct {
			devMode  p9.FileMode
			instance devInstance
		}{
			{p9.ModeBlockDevice, shutdownInst},
			{p9.ModeCharacterDevice, motdInst},
		} {
			if _, err := root.Mknod(motdName, pair.devMode, apiDev, pair.instance, 0, 0); err != nil {
				return err
			}
		}
	return nil
}
*/

/*
func (dir *Directory) Mknod(name string, mode p9.FileMode,
	major uint32, minor uint32,
	uid p9.UID, gid p9.GID,
) (p9.QID, error) {
	if !mode.IsBlockDevice() &&
		!mode.IsCharacterDevice() {
		return p9.QID{}, fmt.Errorf(`mode "%v" does not specify a device`, mode)
	}
	switch major {
	case apiDev:
		return dir.makeDevice(name, minor)
	default:
		return p9.QID{}, fmt.Errorf("bad device-class type: %d want %d", major, apiDev) // TODO: err format
	}
}

func (dir *Directory) makeDevice(name string, instanceType devInstance) (p9.QID, error) {
	switch instanceType {
	case motdInst:
		if _, exists := dir.fileTable[name]; exists {
			return p9.QID{}, perrors.EEXIST // TODO: double check spec - EEXIST is probably right
		}
		// motdDir, qid := motd.NewMOTD([]string{name},dir.path)
		// dir.fileTable[name] = motdDir
		// return qid, nil
		return p9.QID{}, errors.New("NIY")
	case shutdownInst:
		return p9.QID{}, errors.New("NIY")
		// return p9.QID{}, goerrors.New("NIY")
		// return id.Get(p9.TypeTemporary), nil
	default:
		return p9.QID{}, fmt.Errorf("bad device-instance type: %d want %d|%d",
			instanceType, motdInst, shutdownInst) // TODO: err format
	}
}
*/
