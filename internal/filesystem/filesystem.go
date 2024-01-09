package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/generic"
)

type (
	// Host represents a file system host API.
	// (9P, Fuse, et al.)
	Host string
	// ID represents a particular file system implementation.
	// (IPFS, IPNS, et al.)
	ID string

	IDFS interface {
		fs.FS
		ID() ID
	}
	OpenFileFS interface {
		fs.FS
		OpenFile(name string, flag int, perm fs.FileMode) (fs.File, error)
	}
	CreateFileFS interface {
		fs.FS
		CreateFile(name string) (fs.File, error)
	}
	RemoveFS interface {
		fs.FS
		Remove(name string) error
	}
	SymlinkFS interface {
		fs.FS
		Lstat(name string) (fs.FileInfo, error)
		Symlink(oldname, newname string) error
		Readlink(name string) (string, error)
	}
	RenameFS interface {
		fs.FS
		Rename(oldName, newName string) error
	}
	TruncateFileFS interface {
		fs.FS
		Truncate(name string, size int64) error
	}
	MkdirFS interface {
		fs.FS
		Mkdir(name string, perm fs.FileMode) error
	}

	// A StreamDirFile is a directory file whose entries
	// can be received with the StreamDir method.
	StreamDirFile interface {
		fs.ReadDirFile
		// StreamDir shares [fs.ReadDirFile]'s position,
		// and sends entries until either the last entry
		// is sent or the directory is closed.
		StreamDir() <-chan StreamDirEntry
	}
	TruncateFile interface {
		fs.File
		Truncate(size int64) error
	}

	StreamDirEntry interface {
		fs.DirEntry
		Error() error
	}

	AccessTimeInfo interface {
		fs.FileInfo
		AccessTime() time.Time
	}
	ChangeTimeInfo interface {
		fs.FileInfo
		ChangeTime() time.Time
	}
	CreationTimeInfo interface {
		fs.FileInfo
		CreationTime() time.Time
	}

	dirEntryWrapper struct {
		fs.DirEntry
		error
	}
)

// Go file permission bits.
const (
	ExecuteOther fs.FileMode = 1 << iota
	WriteOther
	ReadOther

	ExecuteGroup
	WriteGroup
	ReadGroup

	ExecuteUser
	WriteUser
	ReadUser

	Root = "."

	ErrPath     = generic.ConstError("path not valid")
	ErrNotFound = generic.ConstError("file not found")
	ErrNotOpen  = generic.ConstError("file is not open")
	ErrIsDir    = generic.ConstError("file is a directory")
	ErrIsNotDir = generic.ConstError("file is not a directory")
)

func (dw dirEntryWrapper) Error() error { return dw.error }

func FSID(fsys fs.FS) (ID, error) {
	if fsys, ok := fsys.(IDFS); ok {
		return fsys.ID(), nil
	}
	return "", fmt.Errorf(
		"id %T: %w",
		fsys, errors.ErrUnsupported,
	)
}

func OpenFile(fsys fs.FS, name string, flag int, perm fs.FileMode) (fs.File, error) {
	if fsys, ok := fsys.(OpenFileFS); ok {
		return fsys.OpenFile(name, flag, perm)
	}
	if flag == os.O_RDONLY {
		return fsys.Open(name)
	}
	return nil, fmt.Errorf(`open "%s": operation not supported`, name)
}

func CreateFile(fsys fs.FS, name string) (fs.File, error) {
	if fsys, ok := fsys.(CreateFileFS); ok {
		return fsys.CreateFile(name)
	}
	const op = "createfile"
	return nil, unsupportedOpErr(op, name)
}

func Lstat(fsys fs.FS, name string) (fs.FileInfo, error) {
	if fsys, ok := fsys.(SymlinkFS); ok {
		return fsys.Lstat(name)
	}
	const op = "lstat"
	return nil, unsupportedOpErr(op, name)
}

func Symlink(fsys fs.FS, oldname, newname string) error {
	if fsys, ok := fsys.(SymlinkFS); ok {
		return fsys.Symlink(oldname, newname)
	}
	const op = "symlink"
	return unsupportedOpErr2(op, oldname, newname)
}

func Readlink(fsys fs.FS, name string) (string, error) {
	if fsys, ok := fsys.(SymlinkFS); ok {
		return fsys.Readlink(name)
	}
	const op = "readlink"
	return "", unsupportedOpErr(op, name)
}

func Truncate(fsys fs.FS, name string, size int64) error {
	file, err := OpenFile(fsys, name, os.O_WRONLY|os.O_CREATE, 0o666)
	if err != nil {
		return err
	}
	truncater, ok := file.(TruncateFile)
	if !ok {
		return errors.Join(
			fmt.Errorf(`truncate "%s": operation not supported`, name),
			file.Close(),
		)
	}
	return errors.Join(
		truncater.Truncate(size),
		file.Close(),
	)
}

// StreamDir reads the directory
// and returns a channel of directory entry results.
//
// If `directory` implements [StreamDirFile],
// StreamDir calls `directory.StreamDir`.
// Otherwise, StreamDir calls `directory.ReadDir`
// repeatedly with `count` until the entire directory
// is read, an error is encountered, or the context is done.
func StreamDir(ctx context.Context, count int, directory fs.ReadDirFile) <-chan StreamDirEntry {
	if dirStreamer, ok := directory.(StreamDirFile); ok {
		return dirStreamer.StreamDir()
	}
	var (
		stream = make(chan StreamDirEntry)
		send   = func(res StreamDirEntry) (sent bool) {
			select {
			case stream <- res:
				return true
			case <-ctx.Done():
				return false
			}
		}
	)
	go func() {
		defer close(stream)
		for {
			ents, err := directory.ReadDir(count)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					send(dirEntryWrapper{error: err})
				}
				return
			}
			for _, ent := range ents {
				if ctx.Err() != nil {
					return
				}
				if !send(dirEntryWrapper{DirEntry: ent}) {
					return
				}
			}
		}
	}()
	return stream
}

func unsupportedOpErr(op, name string) error {
	return fmt.Errorf(
		op+` "%s": %w`,
		name, errors.ErrUnsupported,
	)
}

func unsupportedOpErr2(op, name1, name2 string) error {
	return fmt.Errorf(
		op+` "%s" -> "%s": %w`,
		name1, name2, errors.ErrUnsupported,
	)
}
