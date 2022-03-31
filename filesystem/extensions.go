// Package filesystem contains extensions to the Go file system interface.
package filesystem

import (
	"context"
	"fmt"
	"io/fs"
	"os"
)

type OpenFileFS interface { // import "std/russ"
	fs.FS
	OpenFile(name string, flag int, perm os.FileMode) (fs.File, error)
}

func OpenFile(fsys fs.FS, name string, flag int, perm os.FileMode) (fs.File, error) {
	if fsys, ok := fsys.(OpenFileFS); ok {
		return fsys.OpenFile(name, flag, perm)
	}
	if flag == os.O_RDONLY {
		return fsys.Open(name)
	}
	return nil, fmt.Errorf("open %s: operation not supported", name)
}

type RenameFS interface { // import "std/russ"
	fs.FS
	Rename(oldpath, newpath string) error
}

type OpenDirFS interface {
	fs.FS
	OpenDir(name string) (fs.ReadDirFile, error)
}

func Rename(fsys fs.FS, oldpath, newpath string) error {
	if fsys, ok := fsys.(RenameFS); ok {
		return fsys.Rename(oldpath, newpath)
	}

	return fmt.Errorf("rename %s %s: operation not supported", oldpath, newpath)
}

type IdentifiedFS interface {
	fs.FS
	ID() ID
}

// TODO: consider what to name these
type (
	StreamDirFile interface {
		fs.File
		StreamDir(context.Context, chan<- fs.DirEntry) <-chan error
	}
	StreamDirFS interface {
		fs.FS
		OpenStream(name string) (StreamDirFile, error)
	}
)

// TODO: review; hasty
func StreamDir(ctx context.Context,
	directory fs.ReadDirFile, entries chan<- fs.DirEntry,
) <-chan error {
	stream, ok := directory.(StreamDirFile)
	if ok {
		return stream.StreamDir(ctx, entries)
	}

	contents, err := directory.ReadDir(0)
	if err != nil {
		single := make(chan error, 1)
		single <- err
		close(single)
		return single
	}
	go func() {
		defer close(entries)
		for _, ent := range contents {
			if ctx.Err() != nil {
				return
			}
			entries <- ent
		}
	}()
	return nil
}
