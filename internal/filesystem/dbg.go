package filesystem

import (
	"io/fs"
	"log"
	"os"
	"time"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/hugelgupf/p9/p9"
)

type (
	dbgFS struct {
		root rootDirectory
	}
	dbgEntry struct {
		name string
	}
)

func NewDBGFS() dbgFS {
	return dbgFS{
		root: newRoot(s_IRXA, []fs.DirEntry{
			// dbgEntry{"file.exe"},
			staticStat{
				name:    "file.exe",
				size:    498176,
				mode:    s_IRXA,
				modTime: time.Now(),
			},
		}),
	}
}

func (dfs dbgFS) Open(name string) (fs.File, error) {
	if name == rootName {
		return dfs.root, nil
	}

	for _, ent := range dfs.root.ents {
		if ent.Name() == name {
			log.Println("Open: ", name)
			f, err := os.Open(`T:\gtime.exe`)
			if err != nil {
				log.Println("open hit:", err)
				return nil, err
			}
			return f, nil
		}
	}
	log.Println("Open - failing on:", name)
	return nil, &fs.PathError{
		Op:   "open",
		Path: name,
		Err:  fserrors.New(fserrors.NotExist), // TODO old-style err; convert to wrapped, defined, const errs.
	}
}

func (dfs dbgFS) OpenDir(name string) (fs.ReadDirFile, error) {
	if name == rootName {
		return dfs.root, nil
	}
	log.Println("OpenDir: ", name)
	const op fserrors.Op = "dbgFS.Opendir"
	return nil, fserrors.New(op, fserrors.NotDir)
}

func (de dbgEntry) Name() string               { return de.name }
func (de dbgEntry) IsDir() bool                { return false }
func (de dbgEntry) Type() fs.FileMode          { return fs.FileMode(0) | s_IRXA }
func (de dbgEntry) Info() (fs.FileInfo, error) { return de, nil }

func (dbgEntry) ModTime() time.Time { return time.Now() }

func (de dbgEntry) Mode() fs.FileMode { return de.Type() }
func (de dbgEntry) Size() int64       { return 498176 }
func (de dbgEntry) Sys() any          { return de }

func (de dbgEntry) Stat() (fs.FileInfo, error) {
	log.Println("de Stat - type:", de.Type())
	log.Println("de Stat - perm:", fs.ModePerm)
	log.Println("de Stat - mode:", de.Mode())
	return de, nil
}

// func (de dbgEntry) Mode() fs.FileMode { return de.Type() | fs.FileMode(files.S_IRXA) }
const (
	s_IROTH os.FileMode = os.FileMode(p9.Read)
	s_IWOTH             = os.FileMode(p9.Write)
	s_IXOTH             = os.FileMode(p9.Exec)

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

// func (de dbgEntry) Mode() fs.FileMode { return de.Type() | s_IRXA }
