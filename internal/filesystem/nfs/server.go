package nfs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/go-git/go-billy/v5"
)

type (
	netFS struct {
		fsys fs.FS
	}
	netFile struct {
		file      fs.File
		billyName string // "… as presented to Open." -v5 docs.
	}
	// netFileEx extends operation support
	// of basic [fs.File]s.
	netFileEx struct {
		netFile
		curMu sync.Mutex
	}
)

func (ns *netFS) Capabilities() billy.Capability {
	return billy.WriteCapability |
		billy.ReadCapability |
		billy.ReadAndWriteCapability |
		billy.SeekCapability |
		billy.TruncateCapability
}

func toGoPath(filename string) string {
	if filename == "/" {
		return filesystem.Root
	}
	return filename
}

func (ns *netFS) Create(filename string) (billy.File, error) {
	const (
		flag = os.O_RDWR | os.O_CREATE | os.O_TRUNC
		perm = 0o666
	)
	return ns.OpenFile(filename, flag, perm)
}

func (ns *netFS) Open(filename string) (billy.File, error) {
	const (
		flag = os.O_RDONLY
		perm = 0
	)
	return ns.OpenFile(filename, flag, perm)
}

func (ns *netFS) OpenFile(filename string, flag int, perm os.FileMode) (billy.File, error) {
	name := toGoPath(filename)
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  fs.ErrInvalid,
		}
	}
	file, err := filesystem.OpenFile(ns.fsys, name, flag, perm)
	if err != nil {
		return nil, err
	}
	netFile := netFile{
		billyName: filename,
		file:      file,
	}
	if _, ok := file.(io.ReaderAt); ok {
		return &netFile, nil
	}
	return &netFileEx{
		netFile: netFile,
	}, nil
}

func (ns *netFS) Stat(filename string) (fs.FileInfo, error) {
	return fs.Stat(ns.fsys, toGoPath(filename))
}

func (ns *netFS) Rename(oldpath, newpath string) error {
	var (
		oldName = toGoPath(oldpath)
		newName = toGoPath(newpath)
	)
	return filesystem.Rename(ns.fsys, oldName, newName)
}

func (ns *netFS) Remove(filename string) error {
	return filesystem.Remove(ns.fsys, toGoPath(filename))
}

func (ns *netFS) Join(elem ...string) string {
	if len(elem) == 0 {
		return "/"
	}
	return path.Join(elem...)
}

func (ns *netFS) ReadDir(path string) ([]fs.FileInfo, error) {
	ents, err := fs.ReadDir(ns.fsys, toGoPath(path))
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

func (ns *netFS) MkdirAll(filename string, perm os.FileMode) error {
	const goDelimiter = "/"
	var (
		name       = toGoPath(filename)
		fsys       = ns.fsys
		components = strings.Split(name, goDelimiter)
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

func (ns *netFS) Lstat(filename string) (fs.FileInfo, error) {
	return filesystem.Lstat(ns.fsys, toGoPath(filename))
}

func (ns *netFS) Symlink(oldpath, newpath string) error {
	var (
		oldName = toGoPath(oldpath)
		newName = toGoPath(newpath)
	)
	return filesystem.Symlink(ns.fsys, oldName, newName)
}

func (ns *netFS) Readlink(filename string) (string, error) {
	return filesystem.Readlink(ns.fsys, toGoPath(filename))
}

func (nf *netFile) Name() string { return nf.billyName }

func (nf *netFile) Write(p []byte) (n int, err error) {
	if writer, ok := nf.file.(io.Writer); ok {
		return writer.Write(p)
	}
	const op = "write"
	return -1, unsupportedOpErr(op, nf.billyName)
}

func (nf *netFile) Read(p []byte) (int, error) {
	return nf.file.Read(p)
}

func (nf *netFile) ReadAt(p []byte, off int64) (int, error) {
	// NOTE: interface checked during [Open].
	return nf.file.(io.ReaderAt).ReadAt(p, off)
}

func (nf *netFileEx) Read(p []byte) (int, error) {
	nf.curMu.Lock()
	defer nf.curMu.Unlock()
	return nf.file.Read(p)
}

func (nf *netFileEx) ReadAt(p []byte, off int64) (int, error) {
	nf.curMu.Lock()
	defer nf.curMu.Unlock()
	readSeeker, ok := nf.file.(io.ReadSeeker)
	if !ok {
		const op = "readat"
		return -1, unsupportedOpErr(op, nf.billyName)
	}
	return readAtLocked(readSeeker, p, off)
}

func readAtLocked(rs io.ReadSeeker, p []byte, off int64) (int, error) {
	const errno = -1
	were, err := rs.Seek(0, io.SeekCurrent)
	if err != nil {
		return errno, err
	}
	sought, err := rs.Seek(off, io.SeekStart)
	if err != nil {
		return errno, err
	}
	if err := compareOffsets(sought, off); err != nil {
		return errno, err
	}
	n, rErr := rs.Read(p)
	where, err := rs.Seek(were, io.SeekStart)
	if err != nil {
		return errno, errors.Join(err, rErr)
	}
	if err := compareOffsets(where, were); err != nil {
		return errno, errors.Join(err, rErr)
	}
	return n, rErr
}

func (nf *netFile) Seek(offset int64, whence int) (int64, error) {
	if seeker, ok := nf.file.(io.Seeker); ok {
		return seeker.Seek(offset, whence)
	}
	const op = "seek"
	return -1, unsupportedOpErr(op, nf.billyName)
}

func (nf *netFile) Close() error {
	return nf.file.Close()
}

func (nf *netFile) Lock() error {
	const op = "lock"
	return unsupportedOpErr(op, nf.billyName)
}

func (nf *netFile) Unlock() error {
	const op = "unlock"
	return unsupportedOpErr(op, nf.billyName)
}

func (nf *netFile) Truncate(size int64) error {
	if truncater, ok := nf.file.(filesystem.TruncateFile); ok {
		return truncater.Truncate(size)
	}
	const op = "truncate"
	return unsupportedOpErr(op, nf.billyName)
}

func unsupportedOpErr(op, name string) error {
	return fmt.Errorf(
		op+` "%s": %w`,
		name, errors.ErrUnsupported,
	)
}

func compareOffsets(got, want int64) (err error) {
	if got == want {
		return nil
	}
	return fmt.Errorf(
		"offset mismatch got %d expected %d",
		got, want,
	)
}
