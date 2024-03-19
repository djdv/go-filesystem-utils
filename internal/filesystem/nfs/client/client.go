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
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/multiformats/go-multiaddr"
	"github.com/willscott/go-nfs-client/nfs"
)

type (
	// FS represents an NFS client connection
	// as an [fs.FS] with [filesystem] extensions.
	FS struct {
		target        *nfs.Target
		linkSeparator string
		linkLimit     uint
		// NOTE [2024.01.08]: The NFS server library is able to handle multiple requests concurrently
		// but the client library is not intended to handle multiple outstanding operations.
		// As a result, we lock on each operation.
		// If this changes upstream, we can drop the operation mutex.
		opMu sync.Mutex
	}
	goShared struct {
		opMu    *sync.Mutex
		netName string
	}
	goFile struct {
		file *nfs.File
		goShared
	}
	goDirectory struct {
		goShared
		target  *nfs.Target
		entries []*nfs.EntryPlus
	}
	goEnt struct {
		*nfs.EntryPlus
	}
	// goStatWrapper wraps the [fs.FileInfo] returned from
	// [nfs.target.Getattr] (which returns an empty name
	// in its `.Name` method).
	goStatWrapper struct {
		fs.FileInfo
		name string
	}
)

const (
	// ID defines the identifier of this system.
	ID filesystem.ID = "NFS"

	errStale = generic.ConstError("handle became stale")
)

func (*FS) ID() filesystem.ID { return ID }

func New(maddr multiaddr.Multiaddr, options ...Option) (*FS, error) {
	var (
		fsys = FS{
			linkSeparator: DefaultLinkSeparator,
			linkLimit:     DefaultLinkLimit,
		}
		settings = settings{
			dirpath: DefaultDirpath,
			FS:      &fsys,
		}
	)
	if err := generic.ApplyOptions(&settings, options...); err != nil {
		return nil, err
	}
	if err := settings.fillDefaults(); err != nil {
		return nil, err
	}
	target, err := settings.nfsTarget(maddr)
	if err != nil {
		return nil, err
	}
	fsys.target = target
	return &fsys, nil
}

func (fsys *FS) Lstat(name string) (fs.FileInfo, error) {
	fsys.opMu.Lock()
	defer fsys.opMu.Unlock()
	const op = "lstat"
	return getattr(op, fsys.target, name)
}

func getattr(op string, target *nfs.Target, name string) (fs.FileInfo, error) {
	info, err := target.Getattr(name)
	if err != nil {
		return nil, nfsToFsErr(op, name, err)
	}
	return goStatWrapper{
		name:     path.Base(name),
		FileInfo: info,
	}, nil
}

func (fsys *FS) Stat(name string) (fs.FileInfo, error) {
	fsys.opMu.Lock()
	defer fsys.opMu.Unlock()
	const (
		op    = "stat"
		depth = 0
	)
	return fsys.statLocked(op, name, depth)
}

func (fsys *FS) statLocked(op, name string, depth uint) (fs.FileInfo, error) {
	target := fsys.target
	info, err := getattr(op, target, name)
	if err != nil {
		return nil, err
	}
	if isLink := info.Mode().Type()&fs.ModeSymlink != 0; !isLink {
		return info, nil
	}
	if depth++; depth >= fsys.linkLimit {
		return nil, linkLimitError(op, name, fsys.linkLimit)
	}
	resolved, err := fsys.resolveLinkLocked(op, name)
	if err != nil {
		return nil, err
	}
	return fsys.statLocked(op, resolved, depth)
}

func (fsys *FS) resolveLinkLocked(op, name string) (string, error) {
	link, err := fsys.target.Open(name)
	if err != nil {
		return "", nfsToFsErr(op, name, err)
	}
	target, err := link.Readlink()
	if err != nil {
		return "", nfsToFsErr(op, name, err)
	}
	if targetIsInvalid(target) {
		const (
			err  = generic.ConstError("link target must be relative")
			kind = fserrors.InvalidItem
		)
		pair := fmt.Sprintf(
			`%s -> %s`,
			name, target,
		)
		return "", fserrors.New(op, pair, err, kind)
	}
	if sep := fsys.linkSeparator; sep != "" {
		target = strings.ReplaceAll(target, sep, "/")
	}
	return path.Join(
		path.Dir(name),
		target,
	), nil
}

func (in goStatWrapper) Name() string { return in.name }

func (fsys *FS) Create(name string) (fs.File, error) {
	fsys.opMu.Lock()
	defer fsys.opMu.Unlock()
	const perm = 0o666
	file, err := fsys.target.OpenFile(name, perm)
	if err != nil {
		const op = "create"
		return nil, nfsToFsErr(op, name, err)
	}
	return &goFile{
		goShared: goShared{
			netName: name,
			opMu:    &fsys.opMu,
		},
		file: file,
	}, nil
}

func (fsys *FS) Open(name string) (fs.File, error) {
	fsys.opMu.Lock()
	defer fsys.opMu.Unlock()
	const depth = 0
	return fsys.openLocked(name, depth)
}

func (fsys *FS) openLocked(name string, depth uint) (fs.File, error) {
	const op = "open"
	if !fs.ValidPath(name) {
		return nil, fserrors.New(op, name, fs.ErrInvalid, fserrors.InvalidItem)
	}
	var (
		target    = fsys.target
		info, err = target.Getattr(name)
	)
	if err != nil {
		return nil, nfsToFsErr(op, name, err)
	}
	shared := goShared{
		netName: name,
		opMu:    &fsys.opMu,
	}
	switch typ := info.Mode().Type(); {
	case typ.IsRegular():
		file, err := target.Open(name)
		if err != nil {
			return nil, nfsToFsErr(op, name, err)
		}
		return &goFile{
			goShared: shared,
			file:     file,
		}, nil
	case typ.IsDir():
		return &goDirectory{
			goShared: shared,
			target:   fsys.target,
		}, nil
	case typ&fs.ModeSymlink != 0:
		if depth++; depth >= fsys.linkLimit {
			return nil, linkLimitError(op, name, fsys.linkLimit)
		}
		resolved, err := fsys.resolveLinkLocked(op, name)
		if err != nil {
			return nil, err
		}
		return fsys.openLocked(resolved, depth)
	default:
		return nil, fmt.Errorf(
			`open "%s": file type "%v" %w`,
			name, typ, errors.ErrUnsupported,
		)
	}
}

func (fsys *FS) Symlink(oldname, newname string) error {
	if err := fsys.target.Symlink(newname, oldname); err != nil {
		return &os.LinkError{
			Op:  "symlink",
			Old: oldname,
			New: newname,
			Err: err,
		}
	}
	return nil
}

func (fsys *FS) ReadLink(name string) (string, error) {
	fsys.opMu.Lock()
	defer fsys.opMu.Unlock()
	link, err := fsys.target.Open(name)
	if err != nil {
		const op = "readlink"
		return "", nfsToFsErr(op, name, err)
	}
	return link.Readlink()
}

func (f *goFile) refreshLocked() error {
	var (
		target = f.file.Target
		name   = f.netName
	)
	info, err := target.Getattr(name)
	if err != nil {
		return err
	}
	typ := info.Mode().Type()
	if !typ.IsRegular() {
		return fmt.Errorf(
			`refresh "%s": file type changed from regular to %v`,
			name, fsTypeName(typ),
		)
	}
	cur, err := f.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	file, err := target.Open(name)
	if err != nil {
		return err
	}
	sought, err := file.Seek(cur, io.SeekStart)
	if err != nil {
		return err
	}
	if err := compareOffsets(sought, cur); err != nil {
		return err
	}
	f.file = file
	return nil
}

func fsTypeName(typ fs.FileMode) string {
	switch typ {
	case fs.FileMode(0):
		return "regular"
	case fs.ModeDir:
		return "directory"
	case fs.ModeSymlink:
		return "symbolic link"
	case fs.ModeNamedPipe:
		return "named pipe"
	case fs.ModeSocket:
		return "socket"
	case fs.ModeDevice:
		return "device"
	case fs.ModeCharDevice:
		return "character device"
	default:
		return "irregular"
	}
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

func (f *goFile) Stat() (fs.FileInfo, error) {
	f.opMu.Lock()
	defer f.opMu.Unlock()
	const op = "stat"
	return getattr(op, f.file.Target, f.netName)
}

func (f *goFile) Read(p []byte) (int, error) {
	f.opMu.Lock()
	defer f.opMu.Unlock()
	return f.readLocked(p)
}

func (f *goFile) readLocked(p []byte) (int, error) {
	n, err := f.file.Read(p)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return n, err
		}
		const op = "read"
		err = nfsToFsErr(op, f.netName, err)
		if !errors.Is(err, errStale) {
			return n, err
		}
		if err := f.refreshLocked(); err != nil {
			return n, nfsToFsErr(op, f.netName, err)
		}
		return f.readLocked(p)
	}
	return n, nil
}

func (f *goFile) Write(p []byte) (int, error) {
	f.opMu.Lock()
	defer f.opMu.Unlock()
	return f.writeLocked(p)
}

func (f *goFile) writeLocked(p []byte) (int, error) {
	n, err := f.file.Write(p)
	if err != nil {
		const op = "write"
		err = nfsToFsErr(op, f.netName, err)
		if !errors.Is(err, errStale) {
			return n, err
		}
		if err := f.refreshLocked(); err != nil {
			return n, nfsToFsErr(op, f.netName, err)
		}
		return f.writeLocked(p)
	}
	return n, nil
}

func (f *goFile) Seek(offset int64, whence int) (int64, error) {
	f.opMu.Lock()
	defer f.opMu.Unlock()
	off, err := f.file.Seek(offset, whence)
	if err != nil {
		const op = "seek"
		return off, nfsToFsErr(op, f.netName, err)
	}
	return off, nil
}

func (f *goFile) Close() error {
	f.opMu.Lock()
	defer f.opMu.Unlock()
	if err := f.file.Close(); err != nil {
		const op = "close"
		err = nfsToFsErr(op, f.netName, err)
		if !errors.Is(err, errStale) {
			return err
		}
	}
	return nil
}

func (dir *goDirectory) Read([]byte) (int, error) {
	return -1, errors.ErrUnsupported
}

func (dir *goDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	dir.opMu.Lock()
	defer dir.opMu.Unlock()
	entries := dir.entries
	if entries == nil {
		var (
			err    error
			target = dir.target
			name   = dir.netName
		)
		if entries, err = target.ReadDirPlus(name); err != nil {
			const op = "readdir"
			return nil, nfsToFsErr(op, dir.netName, err)
		}
		dir.entries = entries
	}
	entriesLeft := len(entries)
	if entriesLeft == 0 && count > 0 {
		return nil, io.EOF
	}
	if count > 0 && entriesLeft > count {
		entriesLeft = count
	}
	list := make([]fs.DirEntry, entriesLeft)
	for i, ent := range entries[:entriesLeft] {
		list[i] = goEnt{EntryPlus: ent}
	}
	dir.entries = entries[entriesLeft:]
	return list, nil
}

func (dir *goDirectory) Stat() (fs.FileInfo, error) {
	dir.opMu.Lock()
	defer dir.opMu.Unlock()
	const op = "stat"
	return getattr(op, dir.target, dir.netName)
}

func (*goDirectory) Close() error { return nil }

func (ent goEnt) Info() (fs.FileInfo, error) { return &ent.Attr.Attr, nil }

func (ent goEnt) Type() fs.FileMode { return ent.Mode() }

func nfsToFsErr(op, name string, err error) error {
	var kind fserrors.Kind
	switch {
	case errors.Is(err, fs.ErrPermission):
		kind = fserrors.Permission
	case errors.Is(err, fs.ErrExist):
		kind = fserrors.Exist
	case errors.Is(err, fs.ErrNotExist):
		kind = fserrors.NotExist
	default:
		const NFS3ERR_JUKEBOX = 10008
		var nfsError *nfs.Error
		if errors.As(err, &nfsError) {
			switch nfsError.ErrorNum {
			case nfs.NFS3ErrStale:
				return errStale
			case nfs.NFS3ErrInval, nfs.NFS3ErrNameTooLong,
				nfs.NFS3ErrRemote, nfs.NFS3ErrBadType:
				kind = fserrors.InvalidItem
			case nfs.NFS3ErrPerm, nfs.NFS3ErrAcces:
				kind = fserrors.Permission
			case nfs.NFS3ErrIO, nfs.NFS3ErrNXIO,
				nfs.NFS3ErrXDev, nfs.NFS3ErrNoDev,
				nfs.NFS3ErrFBig, nfs.NFS3ErrNoSpc,
				nfs.NFS3ErrMLink, nfs.NFS3ErrDQuot,
				nfs.NFS3ErrBadHandle, nfs.NFS3ErrNotSync,
				nfs.NFS3ErrBadCookie, nfs.NFS3ErrTooSmall,
				nfs.NFS3ErrServerFault, NFS3ERR_JUKEBOX:
				// NOTE: Jukebox is technically a temporary error
				// but we have no analog for those yet.
				kind = fserrors.IO
			case nfs.NFS3ErrIsDir:
				kind = fserrors.IsDir
			case nfs.NFS3ErrNotDir:
				kind = fserrors.NotDir
			case nfs.NFS3ErrNotEmpty:
				kind = fserrors.NotEmpty
			case nfs.NFS3ErrROFS:
				kind = fserrors.ReadOnly
			}
		}
	}
	return fserrors.New(op, name, err, kind)
}

func linkLimitError(op, name string, limit uint) error {
	const kind = fserrors.Recursion
	err := fmt.Errorf(
		"reached symbolic link resolution limit (%d) during operation",
		limit,
	)
	return fserrors.New(op, name, err, kind)
}
