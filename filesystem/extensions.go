// Package filesystem contains extensions to the Go file system interface.
package filesystem

import (
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
	// TODO: reconsider signature
	// We should probably keep this but add another extension
	// for streams too. E.g. (name string, output chan<- dirent).
	// Or keep as-is and extend readdirfile into a directory stream.
	// Allowing the caller to control the buffer size.
	// dirfile.ReadStream(count int, bufferedChannel chan<-...)
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
