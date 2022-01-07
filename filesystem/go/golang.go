package gofs

import (
	"io"
	"io/fs"
	"sort"

	"github.com/djdv/go-filesystem-utils/filesystem/errors"
)

func ReadDir(count int, entries <-chan fs.DirEntry) ([]fs.DirEntry, error) {
	const op errors.Op = "gofs.ReadDir"
	var ents []fs.DirEntry
	if count > 0 {
		// If we're dealing with a finite amount, allocate for it.
		// NOTE: If the caller passes an unreasonably large `count`,
		// we do nothing to protect against OOM.
		// This is to be considered a client-side implementation error
		// and should be fixed caller side.
		ents = make([]fs.DirEntry, 0, count)
	} else {
		// NOTE: [spec] This will cause the loop below to become infinite.
		// This is intended by the fs.FS spec
		count = -1
	}

	var err error
	for ent := range entries {
		if count == 0 {
			break
		}
		ents = append(ents, ent)
		count--
	}
	if count > 0 {
		err = io.EOF
	}

	sort.Sort(entsByName(ents))
	return ents, err
}

type entsByName []fs.DirEntry

func (ents entsByName) Len() int           { return len(ents) }
func (ents entsByName) Swap(i, j int)      { ents[i], ents[j] = ents[j], ents[i] }
func (ents entsByName) Less(i, j int) bool { return ents[i].Name() < ents[j].Name() }
