package p9

import (
	"sort"
	"sync"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/hugelgupf/p9/p9"
)

type (
	fileTableSync struct {
		mu    sync.RWMutex
		files fileTableMap
	}
	fileTableMap map[string]p9.File
)

func newFileTable() *fileTableSync {
	return &fileTableSync{files: make(fileTableMap)}
}

func (ft *fileTableSync) exclusiveStore(name string, file p9.File) bool {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if _, ok := ft.files[name]; ok {
		return false
	}
	ft.files[name] = file
	return true
}

func (ft *fileTableSync) load(name string) (p9.File, bool) {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	file, ok := ft.files[name]
	return file, ok
}

func (ft *fileTableSync) length() int {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return ft.lengthLocked()
}

func (ft *fileTableSync) lengthLocked() int {
	return len(ft.files)
}

func (ft *fileTableSync) flatten(offset uint64, count uint32) ([]string, []p9.File) {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	var (
		i       int
		entries = ft.files
		names   = make([]string, len(entries))
	)
	for name := range entries {
		names[i] = name
		i++
	}
	sort.Strings(names)
	names = names[offset : offset+uint64(generic.Min(len(names), int(count)))]

	files := make([]p9.File, len(names))
	for i, name := range names {
		files[i] = entries[name]
	}
	return names, files
}

func (ft *fileTableSync) to9Ents(offset uint64, count uint32) (p9.Dirents, error) {
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

func (ft *fileTableSync) delete(name string) bool {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	return ft.deleteLocked(name)
}

func (ft *fileTableSync) deleteLocked(name string) bool {
	_, ok := ft.files[name]
	delete(ft.files, name)
	return ok
}
