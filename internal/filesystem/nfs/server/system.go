package nfs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/go-git/go-billy/v5"
)

type server struct {
	fs fs.FS
}

const ID filesystem.Host = "NFS"

func (srv *server) Capabilities() billy.Capability {
	return billy.WriteCapability |
		billy.ReadCapability |
		billy.ReadAndWriteCapability |
		billy.SeekCapability |
		billy.TruncateCapability
}

func (srv *server) Create(filename string) (billy.File, error) {
	const (
		flag = os.O_RDWR | os.O_CREATE | os.O_TRUNC
		perm = 0o666
	)
	return srv.OpenFile(filename, flag, perm)
}

func (srv *server) Open(filename string) (billy.File, error) {
	const (
		flag = os.O_RDONLY
		perm = 0
	)
	return srv.OpenFile(filename, flag, perm)
}

func (srv *server) OpenFile(filename string, flag int, perm os.FileMode) (billy.File, error) {
	name := toGoPath(filename)
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  fs.ErrInvalid,
		}
	}
	goFile, err := filesystem.OpenFile(srv.fs, name, flag, perm)
	if err != nil {
		return nil, err
	}
	file := file{
		billyName: filename,
		goFile:    goFile,
	}
	if _, ok := goFile.(io.ReaderAt); ok {
		return &file, nil
	}
	return &fileEx{
		file: file,
	}, nil
}

func (srv *server) Stat(filename string) (fs.FileInfo, error) {
	return fs.Stat(srv.fs, toGoPath(filename))
}

func (srv *server) Rename(oldpath, newpath string) error {
	var (
		oldName = toGoPath(oldpath)
		newName = toGoPath(newpath)
	)
	return filesystem.Rename(srv.fs, oldName, newName)
}

func (srv *server) Remove(filename string) error {
	return filesystem.Remove(srv.fs, toGoPath(filename))
}

func (srv *server) Join(elem ...string) string {
	if len(elem) == 0 {
		return "/"
	}
	return path.Join(elem...)
}

func (srv *server) ReadDir(path string) ([]fs.FileInfo, error) {
	ents, err := fs.ReadDir(srv.fs, toGoPath(path))
	if err != nil {
		return nil, err
	}
	infos := make([]fs.FileInfo, len(ents))
	for i, ent := range ents {
		info, err := ent.Info()
		if err != nil {
			return nil, err
		}
		infos[i] = info
	}
	return infos, nil
}

func (srv *server) MkdirAll(filename string, perm os.FileMode) error {
	const separator = "/"
	var (
		name       = toGoPath(filename)
		fsys       = srv.fs
		components = strings.Split(name, separator)
	)
	for i, dir := range components {
		var (
			fragment = append(components[:i], dir)
			name     = path.Join(fragment...)
		)
		if err := filesystem.Mkdir(fsys, name, perm); err != nil {
			if !errors.Is(err, fs.ErrExist) {
				return err
			}
		}
	}
	return nil
}

func (srv *server) Lstat(filename string) (fs.FileInfo, error) {
	return filesystem.Lstat(srv.fs, toGoPath(filename))
}

func (srv *server) Symlink(oldpath, newpath string) error {
	var (
		oldName = toGoPath(oldpath)
		newName = toGoPath(newpath)
	)
	return filesystem.Symlink(srv.fs, oldName, newName)
}

func (srv *server) Readlink(filename string) (string, error) {
	return filesystem.Readlink(srv.fs, toGoPath(filename))
}

func toGoPath(filename string) string {
	if filename == "/" {
		return filesystem.Root
	}
	return filename
}
