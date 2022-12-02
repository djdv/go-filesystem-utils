package filesystem

import (
	"context"
	"io/fs"
	"time"
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

	StreamDirFile interface {
		fs.ReadDirFile
		StreamDir(ctx context.Context) <-chan StreamDirEntry
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
