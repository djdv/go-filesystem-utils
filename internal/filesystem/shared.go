package filesystem

import (
	"io"
	"io/fs"
	"log"
	"time"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
)

const rootName = "."

const (
	// Go permission bits,
	// defined with POSIX naming convention.
	//
	// TODO: We have no requirement to conform to POSIX here.
	// These names should be changed to something practical instead.
	s_IXOTH fs.FileMode = 1 << iota
	s_IWOTH
	s_IROTH

	s_IXGRP
	s_IWGRP
	s_IRGRP

	s_IXUSR
	s_IWUSR
	s_IRUSR

	s_IRWXO = s_IROTH | s_IWOTH | s_IXOTH
	s_IRWXG = s_IRGRP | s_IWGRP | s_IXGRP
	s_IRWXU = s_IRUSR | s_IWUSR | s_IXUSR

	// Non-standard.

	s_IXA   = s_IXUSR | s_IXGRP | s_IXOTH
	s_IWA   = s_IWUSR | s_IWGRP | s_IWOTH
	s_IRA   = s_IRUSR | s_IRGRP | s_IROTH
	s_IRXA  = s_IRA | s_IXA
	s_IRWA  = s_IRA | s_IWA
	s_IRWXA = s_IRWXU | s_IRWXG | s_IRWXO
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

func (se staticStat) Name() string               { return se.name }
func (se staticStat) Size() int64                { return se.size }
func (se staticStat) Mode() fs.FileMode          { return se.mode }
func (se staticStat) Type() fs.FileMode          { return se.mode.Type() }
func (se staticStat) ModTime() time.Time         { return se.modTime }
func (se staticStat) IsDir() bool                { return se.mode.IsDir() }
func (se staticStat) Sys() any                   { return se }
func (se staticStat) Info() (fs.FileInfo, error) { return se, nil }
