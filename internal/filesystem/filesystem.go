package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
)

type (
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
)

func (dw dirEntryWrapper) Error() error { return dw.error }

func OpenFile(fsys fs.FS, name string, flag int, perm fs.FileMode) (fs.File, error) {
	if fsys, ok := fsys.(OpenFileFS); ok {
		return fsys.OpenFile(name, flag, perm)
	}
	if flag == os.O_RDONLY {
		return fsys.Open(name)
	}
	return nil, fmt.Errorf(`open "%s": operation not supported`, name)
}

func Truncate(fsys fs.FS, name string, size int64) (err error) {
	file, err := OpenFile(fsys, name, os.O_WRONLY|os.O_CREATE, 0o666)
	if err != nil {
		return err
	}
	defer func() { err = fserrors.Join(err, file.Close()) }()
	truncater, ok := file.(TruncateFile)
	if ok {
		return truncater.Truncate(size)
	}
	return fmt.Errorf(`truncate "%s": operation not supported`, name)
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
