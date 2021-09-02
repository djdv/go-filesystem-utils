package filesystem

import (
	"fmt"
	"io/fs"
	"os"
)

// FS extends Go's FS interface.
// NOTE: This interface predates the Go standard FS draft and implementation.
// It is in the process of being migrated to conform to its ideals rather than the ad-hoc standards we created prior.
type FS interface {
	fs.FS
	fs.StatFS

	IdentifiedFS
	OpenFileFS
	RenameFS

	// TODO: finish porting these to extensions

	//ExtractLink(path string) (string, error)

	// creation
	//Make(path string) error
	//MakeDirectory(path string) error
	//MakeLink(path, target string) error

	// removal
	//Remove(path string) error
	//RemoveDirectory(path string) error
	//RemoveLink(path string) error

	// node
	//Close() error
}

// File System extensions.
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
