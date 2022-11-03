package files

import (
	"sort"
	"sync"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/hugelgupf/p9/p9"
)

type (
	// TODO: [audit] We probably don't need all these table methods. This is what we had already.
	fileTable interface {
		exclusiveStore(name string, file p9.File) bool
		load(name string) (p9.File, bool)
		length() int
		flatten(offset uint64, count uint32) ([]string, []p9.File)
		to9Ents(offset uint64, count uint32) (p9.Dirents, error)
		delete(name string) bool
	}
	tableSync struct {
		mu    sync.RWMutex
		table mapTable
	}
	mapTable map[string]p9.File
)

// TODO: alloc hint? Lots of device directories will have single to few entries.
// Some user dirs may store their element count so it is known ahead of time.
func newFileTable() *tableSync { return &tableSync{table: make(mapTable)} }

func (ft *tableSync) exclusiveStore(name string, file p9.File) bool {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if _, ok := ft.table[name]; ok {
		return false
	}
	ft.table[name] = file
	return true
}

func (ft *tableSync) load(name string) (p9.File, bool) {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	file, ok := ft.table[name]
	return file, ok
}

func (ft *tableSync) length() int {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return len(ft.table)
}

func (ft *tableSync) flatten(offset uint64, count uint32) ([]string, []p9.File) {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	var (
		i       int
		entries = ft.table
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

func (ft *tableSync) to9Ents(offset uint64, count uint32) (p9.Dirents, error) {
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

func (ft *tableSync) delete(name string) bool {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	_, ok := ft.table[name]
	delete(ft.table, name)
	return ok
}