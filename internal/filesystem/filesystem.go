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
	"github.com/djdv/go-filesystem-utils/internal/generic"
)

type (
	// Host represents a file system host API.
	// (9P, Fuse, et al.)
	Host string
	// ID represents a particular file system implementation.
	// (IPFS, IPNS, et al.)
	ID string

	// IDFS provides an identifier for the file system.
	// I.e. the file system's type.
	IDFS interface {
		fs.FS
		ID() ID
	}
	// OpenFileFS extends an [fs.FS] to provide
	// functionality matching [os.OpenFile].
	OpenFileFS interface {
		fs.FS
		OpenFile(name string, flag int, perm fs.FileMode) (fs.File, error)
	}
	// CreateFileFS extends an [fs.FS] to provide
	// functionality matching [os.Create].
	CreateFileFS interface {
		fs.FS
		Create(name string) (fs.File, error)
	}
	// RemoveFS extends an [fs.FS] to provide
	// functionality matching [os.Remove].
	RemoveFS interface {
		fs.FS
		Remove(name string) error
	}
	// SymlinkFS extends an [fs.FS] to provide
	// functionality matching symlink read operations
	// matching their counterparts in [os].
	SymlinkFS interface {
		fs.FS
		// ReadLink returns the destination of the named symbolic link.
		ReadLink(name string) (string, error)
		// Lstat returns a FileInfo describing the file without following any symbolic links.
		// If there is an error, it should be of type [*fs.PathError].
		Lstat(name string) (fs.FileInfo, error)
	}
	// WritableSymlinkFS extends [SymlinkFS] to provide
	// functionality matching [os.Symlink].
	WritableSymlinkFS interface {
		SymlinkFS
		Symlink(oldname, newname string) error
	}
	// RenameFS extends an [fs.FS] to provide
	// functionality matching [os.Rename].
	RenameFS interface {
		fs.FS
		Rename(oldName, newName string) error
	}
	// TruncateFS extends an [fs.FS] to provide
	// functionality matching [os.Truncate].
	TruncateFS interface {
		fs.FS
		Truncate(name string, size int64) error
	}
	// MkdirFS extends an [fs.FS] to provide
	// functionality matching [os.Mkdir].
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
	// A TruncateFile is a file whose Truncate method
	// matches that of [os.File].
	TruncateFile interface {
		fs.File
		Truncate(size int64) error
	}

	// StreamDirEntry is a directory
	// entry result. Containing either
	// the entry, or the error encountered
	// while enumerating the directory.
	StreamDirEntry interface {
		fs.DirEntry
		Error() error
	}

	// AccessTimeInfo provides the
	// time a file was last accessed.
	AccessTimeInfo interface {
		fs.FileInfo
		AccessTime() time.Time
	}
	// ChangeTimeInfo provides the
	// time a file's info was last modified.
	ChangeTimeInfo interface {
		fs.FileInfo
		ChangeTime() time.Time
	}
	// CreationTimeInfo provides the
	// time a file was created.
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

const (
	Root = "."

	ErrIsDir    = generic.ConstError("file is a directory")
	ErrIsNotDir = generic.ConstError("file is not a directory")
)

func (dw dirEntryWrapper) Error() error { return dw.error }

// FSID calls the [IDFS] extension method
// if present, otherwise returns a wrapped
// [errors.ErrUnsupported].
func FSID(fsys fs.FS) (ID, error) {
	if fsys, ok := fsys.(IDFS); ok {
		return fsys.ID(), nil
	}
	const op = "id"
	return "", unsupportedOpErrAnonymous(op, fsys)
}

// Close calls the [io.Closer] extension method
// if present, otherwise returns `nil`.
func Close(fsys fs.FS) error {
	if closer, ok := fsys.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// OpenFile calls the [OpenFileFS] extension method
// if present, otherwise returns a wrapped
// [errors.ErrUnsupported].
func OpenFile(fsys fs.FS, name string, flag int, perm fs.FileMode) (fs.File, error) {
	if fsys, ok := fsys.(OpenFileFS); ok {
		return fsys.OpenFile(name, flag, perm)
	}
	if flag == os.O_RDONLY {
		return fsys.Open(name)
	}
	const op = "open"
	return nil, unsupportedOpErr(op, name)
}

// CreateFile calls the [CreateFileFS] extension method
// if present, otherwise returns a wrapped
// [errors.ErrUnsupported].
func CreateFile(fsys fs.FS, name string) (fs.File, error) {
	if fsys, ok := fsys.(CreateFileFS); ok {
		return fsys.Create(name)
	}
	const op = "createfile"
	return nil, unsupportedOpErr(op, name)
}

// Remove calls the [RemoveFS] extension method
// if present, otherwise returns a wrapped
// [errors.ErrUnsupported].
func Remove(fsys fs.FS, name string) error {
	if fsys, ok := fsys.(RemoveFS); ok {
		return fsys.Remove(name)
	}
	const op = "remove"
	return unsupportedOpErr(op, name)
}

// Lstat calls the [LinkStater] extension method
// if present, otherwise returns a wrapped
// [errors.ErrUnsupported].
func Lstat(fsys fs.FS, name string) (fs.FileInfo, error) {
	if fsys, ok := fsys.(SymlinkFS); ok {
		return fsys.Lstat(name)
	}
	const op = "lstat"
	return nil, unsupportedOpErr(op, name)
}

// Symlink calls the [LinkMaker] extension method
// if present, otherwise returns a wrapped
// [errors.ErrUnsupported].
func Symlink(fsys fs.FS, oldname, newname string) error {
	if fsys, ok := fsys.(WritableSymlinkFS); ok {
		return fsys.Symlink(oldname, newname)
	}
	const op = "symlink"
	return unsupportedOpErr2(op, oldname, newname)
}

// Readlink calls the [LinkReader] extension method
// if present, otherwise returns a wrapped
// [errors.ErrUnsupported].
func Readlink(fsys fs.FS, name string) (string, error) {
	if fsys, ok := fsys.(SymlinkFS); ok {
		return fsys.ReadLink(name)
	}
	const op = "readlink"
	return "", unsupportedOpErr(op, name)
}

// Rename calls the [RenameFS] extension method
// if present, otherwise returns a wrapped
// [errors.ErrUnsupported].
func Rename(fsys fs.FS, oldName, newName string) error {
	if fsys, ok := fsys.(RenameFS); ok {
		return fsys.Rename(oldName, newName)
	}
	const op = "rename"
	return unsupportedOpErr2(op, oldName, newName)
}

// Truncate calls the [TruncateFile] extension method
// if present, otherwise returns a wrapped
// [errors.ErrUnsupported].
func Truncate(fsys fs.FS, name string, size int64) error {
	file, err := OpenFile(fsys, name, os.O_WRONLY|os.O_CREATE, 0o666)
	if err != nil {
		return err
	}
	if fsys, ok := file.(TruncateFile); ok {
		return errors.Join(
			fsys.Truncate(size),
			file.Close(),
		)
	}
	const op = "truncate"
	return errors.Join(
		unsupportedOpErr(op, name),
		file.Close(),
	)
}

// Mkdir calls the [MkdirFS] extension method
// if present, otherwise returns a wrapped
// [errors.ErrUnsupported].
func Mkdir(fsys fs.FS, name string, perm fs.FileMode) error {
	if fsys, ok := fsys.(MkdirFS); ok {
		return fsys.Mkdir(name, perm)
	}
	const op = "mkdir"
	return unsupportedOpErr(op, name)
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

// Seek calls the [io.Seeker] extension method
// if present, otherwise returns a wrapped
// [errors.ErrUnsupported].
func Seek(file fs.File, offset int64, whence int) (int64, error) {
	if seeker, ok := file.(io.Seeker); ok {
		return seeker.Seek(offset, whence)
	}
	const op = "seek"
	return -1, unsupportedOpErrAnonymous(op, file)
}

func unsupportedOpErr(op, name string) error {
	return fserrors.New(op, name, errors.ErrUnsupported, fserrors.InvalidOperation)
}

func unsupportedOpErr2(op, name1, name2 string) error {
	name := fmt.Sprintf(
		`"%s" -> "%s"`,
		name1, name2,
	)
	return fserrors.New(op, name, errors.ErrUnsupported, fserrors.InvalidOperation)
}

func unsupportedOpErrAnonymous(op string, subject any) error {
	name := fmt.Sprintf("%T", subject)
	return unsupportedOpErr(op, name)
}
