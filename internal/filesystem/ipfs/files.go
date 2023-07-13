package ipfs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/ipfs/boxo/mfs"
	shell "github.com/ipfs/go-ipfs-api"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	FilesFS struct {
		ctx    context.Context
		cancel context.CancelFunc
		shell  *shell.Shell
		info   nodeInfo
	}
	FilesOption  func(*FilesFS) error
	filesOptions []FilesOption
	filesShared  struct {
		ctx         context.Context
		cancel      context.CancelFunc
		shell       *shell.Shell
		mountTime   *time.Time // Substitute for mtime (no data in UFSv1).
		path        string
		permissions fs.FileMode // (No data in UFSv1.)
	}
	filesDirectory struct {
		filesShared
		entries []*shell.MfsLsEntry
	}
	filesFile struct {
		root *FilesFS
		filesShared
		cursor int64
	}
	filesInfo  struct{ nodeInfo }
	filesEntry struct {
		mountTime *time.Time // Substitute for mtime (no data in UFS 1).
		*shell.MfsLsEntry
		parent      string
		permissions fs.FileMode
	}
)

const (
	FilesID   = "IPFSFiles"
	filesRoot = "/"
)

var (
	_ fs.FS                     = (*FilesFS)(nil)
	_ fs.StatFS                 = (*FilesFS)(nil)
	_ filesystem.MkdirFS        = (*FilesFS)(nil)
	_ filesystem.OpenFileFS     = (*FilesFS)(nil)
	_ filesystem.CreateFileFS   = (*FilesFS)(nil)
	_ filesystem.TruncateFileFS = (*FilesFS)(nil)
	_ filesystem.RenameFS       = (*FilesFS)(nil)
	_ filesystem.RemoveFS       = (*FilesFS)(nil)
	_ fs.File                   = (*filesFile)(nil)
	_ io.Writer                 = (*filesFile)(nil)
	_ filesystem.TruncateFile   = (*filesFile)(nil)
	_ fs.ReadDirFile            = (*filesDirectory)(nil)
)

func NewFilesFS(ctx context.Context, maddr multiaddr.Multiaddr, options ...FilesOption) (*FilesFS, error) {
	shell, err := filesClient(maddr)
	if err != nil {
		return nil, err
	}
	var (
		fsCtx, cancel = context.WithCancel(ctx)
		fsys          = FilesFS{
			ctx:    fsCtx,
			cancel: cancel,
			shell:  shell,
			info: nodeInfo{
				name:    rootName,
				modTime: time.Now(),
				mode: fs.ModeDir |
					readAll | executeAll,
			},
		}
	)
	if err := generic.ApplyOptions(&fsys, options...); err != nil {
		cancel()
		return nil, err
	}
	return &fsys, nil
}

func (fsys *FilesFS) setPermissions(permissions fs.FileMode) {
	fsys.info.mode = fsys.info.mode.Type() | permissions.Perm()
}

func (fsys *FilesFS) Open(name string) (fs.File, error) {
	const op = "open"
	var (
		shellName = fsToFilesShell(name)
		info, err = filesStat(
			fsys.ctx, op,
			fsys.shell, shellName,
			fsys.info.modTime, fsys.info.mode.Perm(),
		)
	)
	if err != nil {
		return nil, err
	}
	var (
		ctx, cancel = context.WithCancel(fsys.ctx)
		shared      = filesShared{
			ctx: ctx, cancel: cancel,
			shell:       fsys.shell,
			path:        shellName,
			mountTime:   &fsys.info.modTime,
			permissions: fsys.info.mode.Perm(),
		}
	)
	if info.IsDir() {
		return &filesDirectory{
			filesShared: shared,
		}, nil
	}
	return &filesFile{
		root:        fsys,
		filesShared: shared,
	}, nil
}

func (fsys *FilesFS) Stat(name string) (fs.FileInfo, error) {
	const op = "stat"
	return filesStat(
		fsys.ctx, op,
		fsys.shell, fsToFilesShell(name),
		fsys.info.modTime, fsys.info.mode.Perm(),
	)
}

func (fsys *FilesFS) Mkdir(name string, perm fs.FileMode) error {
	return fsys.shell.FilesMkdir(fsys.ctx, fsToFilesShell(name))
}

func (fsys *FilesFS) OpenFile(name string, flag int, perm fs.FileMode) (fs.File, error) {
	const op = "openfile"
	if flag&os.O_CREATE != 0 {
		_, err := fsys.Stat(name)
		exists := err == nil
		if exists {
			if flag&os.O_EXCL != 0 {
				err = generic.ConstError("file exists but O_EXCL flag provided")
				return nil, newFSError(op, name, err, fserrors.Exist)
			}
			return fsys.Open(name)
		}
		return fsys.CreateFile(name)
	}
	return fsys.Open(name)
}

func (fsys *FilesFS) CreateFile(name string) (fs.File, error) {
	err := fsys.shell.FilesWrite(
		fsys.ctx, fsToFilesShell(name), bytes.NewReader(nil),
		shell.FilesWrite.Create(true),
		shell.FilesWrite.Truncate(true),
	)
	if err != nil {
		return nil, err
	}
	return fsys.Open(name)
}

func (fsys *FilesFS) Truncate(name string, size int64) error {
	file, err := fsys.Open(name)
	if err != nil {
		return err
	}
	var errs []error
	if err := truncateFilesFile(fsys.ctx, fsys.shell,
		file, fsToFilesShell(name), size); err != nil {
		errs = append(errs, err)
	}
	if err := file.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (fsys *FilesFS) Rename(oldName, newName string) error {
	return fsys.shell.FilesMv(fsys.ctx,
		fsToFilesShell(oldName), fsToFilesShell(newName),
	)
}

func (fsys *FilesFS) Remove(name string) error {
	const force = true
	return fsys.shell.FilesRm(fsys.ctx, fsToFilesShell(name), force)
}

func (fsh *filesShared) Stat() (fs.FileInfo, error) {
	const op = "stat"
	return filesStat(
		fsh.ctx, op,
		fsh.shell, fsh.path,
		*fsh.mountTime, fsh.permissions,
	)
}

func (fi *filesInfo) Name() string {
	return filesShellToFs(fi.name)
}

func (fd *filesDirectory) Read([]byte) (int, error) {
	const op = "filesDirectory.Read"
	return -1, newFSError(op, filesShellToFs(fd.path), ErrIsDir, fserrors.IsDir)
}

func (fd *filesDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	mfsEnts := fd.entries
	if mfsEnts == nil {
		var err error
		if mfsEnts, err = fd.shell.FilesLs(fd.ctx, fd.path, shell.FilesLs.Stat(true)); err != nil {
			return nil, err
		}
		fd.entries = mfsEnts
	}
	limit := len(mfsEnts)
	if limit == 0 && count > 0 {
		return nil, io.EOF
	}
	if count > 0 && limit > count {
		limit = count
	}
	entries := make([]fs.DirEntry, limit)
	for i := range entries {
		entries[i] = filesEntry{
			parent:      fd.path,
			mountTime:   fd.mountTime,
			permissions: fd.permissions,
			MfsLsEntry:  mfsEnts[i],
		}
	}
	fd.entries = mfsEnts[limit:]
	return entries, nil
}

func (fd *filesDirectory) Close() error { fd.cancel(); return nil }

func (fe filesEntry) Name() string { return fe.MfsLsEntry.Name }
func (fe filesEntry) IsDir() bool  { return mfs.NodeType(fe.MfsLsEntry.Type) == mfs.TDir }
func (fe filesEntry) Type() (mode fs.FileMode) {
	if fe.IsDir() {
		mode |= fs.ModeDir
	}
	return mode
}

func (fe filesEntry) Info() (fs.FileInfo, error) {
	return &nodeInfo{
		modTime: *fe.mountTime,
		name:    fe.Name(),
		size:    int64(fe.MfsLsEntry.Size),
		mode:    fe.Type() | fe.permissions,
	}, nil
}

func (ff *filesFile) Read(p []byte) (int, error) {
	reader, err := ff.shell.FilesRead(
		ff.ctx, ff.path,
		shell.FilesRead.Count(int64(len(p))),
		shell.FilesRead.Offset(ff.cursor),
	)
	if err != nil {
		return -1, err
	}
	var errs []error
	n, err := reader.Read(p)
	if err != nil {
		errs = append(errs, err)
	}
	if err := reader.Close(); err != nil {
		errs = append(errs, err)
	}
	ff.cursor += int64(n)
	return n, errors.Join(errs...)
}

func (ff *filesFile) Truncate(size int64) error {
	return ff.root.Truncate(filesShellToFs(ff.path), size)
}

func (ff *filesFile) Write(p []byte) (int, error) {
	err := ff.shell.FilesWrite(
		ff.ctx, ff.path,
		bytes.NewReader(p), shell.FilesWrite.Offset(ff.cursor),
	)
	// TODO: concern
	// we don't know how many bytes were actually written
	// Is there any way to get the API to tell us?
	if err != nil {
		return -1, err
	}
	written := len(p)
	ff.cursor += int64(written)
	return written, nil
}

func (ff *filesFile) Seek(offset int64, whence int) (int64, error) {
	const op = "seek"
	switch whence {
	case io.SeekStart:
		if offset < 0 {
			err := generic.ConstError(
				"tried to seek to a position before the beginning of the file",
			)
			return -1, newFSError(
				op, filesShellToFs(ff.path),
				err, fserrors.InvalidItem,
			)
		}
		ff.cursor = offset
	case io.SeekCurrent:
		ff.cursor += offset
	case io.SeekEnd:
		info, err := ff.Stat()
		if err != nil {
			return -1, err
		}
		end := info.Size()
		ff.cursor = end + offset
	}
	return ff.cursor, nil
}

func (f *filesFile) Close() error { f.cancel(); return nil }

func filesClient(apiMaddr multiaddr.Multiaddr) (*shell.Shell, error) {
	address, client, err := prepareClientTransport(apiMaddr)
	if err != nil {
		return nil, err
	}
	return shell.NewShellWithClient(address, client), nil
}

func prepareClientTransport(apiMaddr multiaddr.Multiaddr) (string, *http.Client, error) {
	// TODO: magic number; decide on good timeout and const it.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resolvedMaddr, err := resolveMaddr(ctx, apiMaddr)
	if err != nil {
		return "", nil, err
	}

	// TODO: I think the upstream package needs a patch to handle this internally.
	// we'll hack around it for now. Investigate later.
	// (When trying to use a unix socket for the IPFS maddr
	// the client returned from httpapi.NewAPI will complain on requests - forgot to copy the error lol)
	network, address, err := manet.DialArgs(resolvedMaddr)
	if err != nil {
		return "", nil, err
	}
	switch network {
	default:
		client := &http.Client{
			Transport: &http.Transport{
				Proxy:             http.ProxyFromEnvironment,
				DisableKeepAlives: true,
			},
		}
		return address, client, nil
	case "unix":
		address, client := udsHTTPClient(address)
		return address, client, nil
	}
}

func fsToFilesShell(name string) string {
	if name == rootName {
		return filesRoot
	}
	return filesRoot + name
}

func filesShellToFs(name string) string {
	if name == filesRoot {
		return rootName
	}
	const leadingSlash = 1
	return name[leadingSlash:]
}

func filesStat(ctx context.Context, op string,
	sh *shell.Shell, path string,
	modTime time.Time, permissions fs.FileMode,
) (*nodeInfo, error) {
	stat, err := sh.FilesStat(ctx, path)
	if err != nil {
		var shellErr *shell.Error
		if errors.As(err, &shellErr) {
			if strings.Contains(shellErr.Message, "not exist") {
				return nil, newFSError(
					op, filesShellToFs(path),
					err, fserrors.NotExist,
				)
			}
		}
		return nil, newFSError(op, path, err, fserrors.IO)
	}
	mode := permissions.Perm()
	if stat.Type == "directory" {
		mode |= fs.ModeDir
	}
	return &nodeInfo{
		modTime: modTime,
		name:    path,
		size:    int64(stat.Size),
		mode:    mode,
	}, nil
}

func truncateFilesFile(ctx context.Context,
	sh *shell.Shell, file fs.File, path string, size int64,
) error {
	var (
		errs   []error
		buffer bytes.Buffer
		_, err = io.CopyN(&buffer, file, size)
	)
	buffer.Grow(int(size))
	if err != nil {
		errs = append(errs, err)
	}
	if err := file.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	return sh.FilesWrite(
		ctx, path, &buffer,
		shell.FilesWrite.Truncate(true),
	)
}
