package filesystem

import (
	"io"
	"io/fs"
	"log"
	"os"
	"time"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
)

const (
	rootName = "."

	// Go permission bits,
	// defined with POSIX naming convention.
	//
	// TODO: We have no requirement to conform to POSIX here.
	// These names should be changed to something practical instead.

	s_IROTH os.FileMode = 0o4
	s_IWOTH             = 0o2
	s_IXOTH             = 0o1

	i_modeShift = 3

	s_IRGRP = s_IROTH << i_modeShift
	s_IWGRP = s_IWOTH << i_modeShift
	s_IXGRP = s_IXOTH << i_modeShift

	s_IRUSR = s_IRGRP << i_modeShift
	s_IWUSR = s_IWGRP << i_modeShift
	s_IXUSR = s_IXGRP << i_modeShift

	s_IRWXO = s_IROTH | s_IWOTH | s_IXOTH
	s_IRWXG = s_IRGRP | s_IWGRP | s_IXGRP
	s_IRWXU = s_IRUSR | s_IWUSR | s_IXUSR

	// Non-standard.

	s_IRWXA = s_IRWXU | s_IRWXG | s_IRWXO              // 0777
	s_IRXA  = s_IRWXA &^ (s_IWUSR | s_IWGRP | s_IWOTH) // 0555
)

type (
	rootStat struct { // TODO: remove
		name        string
		permissions fs.FileMode
		modtime     time.Time
	}
	staticStat struct {
		name    string
		size    int64
		mode    fs.FileMode
		modTime time.Time
	}

	rootDirectory struct { // TODO: remove
		stat fs.FileInfo
		ents []fs.DirEntry
	}

	entsByName []fs.DirEntry

	statFunc func() (fs.FileInfo, error)
)

func newRoot(permissions fs.FileMode, ents []fs.DirEntry) rootDirectory {
	return rootDirectory{
		stat: newRootStat(permissions),
		ents: ents,
	}
}

func (rd rootDirectory) Open(name string) (fs.File, error) {
	if name == rootName {
		return rd, nil
	}
	const op fserrors.Op = "rootDirectory.Open"
	return nil, fserrors.New(op, fserrors.IsDir)
}

func (rd rootDirectory) OpenDir(name string) (fs.ReadDirFile, error) {
	log.Println("OpenDir: ", name)
	if name == rootName {
		return rd, nil
	}
	const op fserrors.Op = "rootDirectory.Opendir"
	return nil, fserrors.New(op, fserrors.NotDir)
}

func (rd rootDirectory) Stat() (fs.FileInfo, error) { return rd.stat, nil }

func (rd rootDirectory) Read([]byte) (int, error) {
	const op fserrors.Op = "rootDirectory.Read"
	return -1, fserrors.New(op, fserrors.IsDir)
}

func (rd rootDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	var (
		rootEnts = rd.ents
		ents     = make([]fs.DirEntry, 0, generic.Min(count, len(rootEnts)))
	)
	if count == 0 {
		count-- // Intentionally bypass break condition / append all ents.
	}
	for _, ent := range rootEnts {
		if count == 0 {
			break
		}
		ents = append(ents, ent)
		count--
	}
	if count > 0 {
		return ents, io.EOF
	}
	return ents, nil
}

func (rd rootDirectory) Close() error { return nil }

func newRootStat(permissions fs.FileMode) rootStat {
	return rootStat{
		name:        rootName,
		permissions: permissions,
		modtime:     time.Now(),
	}
}

func (rs rootStat) Name() string       { return rs.name }
func (rs rootStat) Size() int64        { return 0 }
func (rs rootStat) Mode() fs.FileMode  { return fs.ModeDir | rs.permissions }
func (rs rootStat) ModTime() time.Time { return rs.modtime }
func (rs rootStat) IsDir() bool        { return rs.Mode().IsDir() }
func (rs rootStat) Sys() interface{}   { return rs }

func (ents entsByName) Len() int           { return len(ents) }
func (ents entsByName) Swap(i, j int)      { ents[i], ents[j] = ents[j], ents[i] }
func (ents entsByName) Less(i, j int) bool { return ents[i].Name() < ents[j].Name() }

func (se staticStat) Name() string               { return se.name }
func (se staticStat) Size() int64                { return se.size }
func (se staticStat) Mode() fs.FileMode          { return se.mode }
func (se staticStat) Type() fs.FileMode          { return se.mode.Type() }
func (se staticStat) ModTime() time.Time         { return se.modTime }
func (se staticStat) IsDir() bool                { return se.mode.IsDir() }
func (se staticStat) Sys() any                   { return se }
func (se staticStat) Info() (fs.FileInfo, error) { return se, nil }

/*
func (de staticDirEnt) Stat() (fs.FileInfo, error) {
	log.Println("de Stat - type:", de.Type())
	log.Println("de Stat - perm:", fs.ModePerm)
	log.Println("de Stat - mode:", de.Mode())
	return de, nil
}
*/

/*
func ReadDir(count int, entries <-chan fs.DirEntry) ([]fs.DirEntry, error) {
	var ents []fs.DirEntry
	if count > 0 {
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
*/
