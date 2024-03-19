package pinfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"sync/atomic"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	intp "github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/internal"
	coreiface "github.com/ipfs/boxo/coreiface"
	coreoptions "github.com/ipfs/boxo/coreiface/options"
	corepath "github.com/ipfs/boxo/coreiface/path"
)

type (
	pinDirectoryInfo struct {
		modTime     atomic.Pointer[time.Time]
		permissions fs.FileMode
	}
	pinShared struct {
		api  coreiface.PinAPI
		dag  coreiface.APIDagService
		ipfs fs.FS
		info pinDirectoryInfo
	}
	FS struct {
		ctx    context.Context
		cancel context.CancelFunc
		statFn func(*pinDirEntry) error
		pinShared
		snapshot   []filesystem.StreamDirEntry
		apiTimeout time.Duration
		expiry     time.Duration
		cacheMu    sync.RWMutex
	}
	pinDirectory struct {
		*pinShared
		stream *intp.EntryStream
		err    error
	}
	pinDirEntry struct {
		coreiface.Pin
		modTime time.Time
		mode    fs.FileMode
		size    int64
	}
	statFunc func(fs.FS, string) (fs.FileInfo, error)
)

// ID defines the identifier of this system.
const ID filesystem.ID = "PinFS"

// New constructs an [FS] using the defaults listed in the pkg constants.
// A list of [Option] values can be provided to change these defaults as desired.
func New(pinAPI coreiface.PinAPI, options ...Option) (*FS, error) {
	fsys := FS{
		apiTimeout: DefaultAPITimeout,
		expiry:     DefaultCacheExpiry,
		pinShared: pinShared{
			api: pinAPI,
			info: pinDirectoryInfo{
				permissions: DefaultPermissions,
			},
		},
	}
	fsys.info.modTime.Store(new(time.Time))
	for _, setter := range options {
		if err := setter(&fsys); err != nil {
			return nil, err
		}
	}
	if fsys.ctx == nil {
		fsys.ctx, fsys.cancel = context.WithCancel(context.Background())
	}
	fsys.initStatFunc()
	return &fsys, nil
}

func (fsys *FS) initStatFunc() {
	var (
		ipfs        = fsys.ipfs
		permissions = &fsys.info.permissions
	)
	if ipfs == nil {
		fsys.statFn = func(entry *pinDirEntry) error {
			entry.mode = permissions.Perm()
			entry.modTime = time.Now()
			return nil
		}
		return
	}
	fsys.statFn = func(entry *pinDirEntry) error {
		name := entry.Path().Cid().String()
		info, err := fs.Stat(ipfs, name)
		if err != nil {
			return err
		}
		entry.mode = info.Mode() | permissions.Perm()
		entry.size = info.Size()
		entry.modTime = info.ModTime()
		return nil
	}
}

func (*FS) ID() filesystem.ID { return ID }

func (fsys *FS) Lstat(name string) (fs.FileInfo, error) {
	const op = "lstat"
	return fsys.stat(op, name, filesystem.Lstat)
}

func (fsys *FS) Stat(name string) (fs.FileInfo, error) {
	const op = "stat"
	return fsys.stat(op, name, fs.Stat)
}

func (fsys *FS) stat(op, name string, statFn statFunc) (fs.FileInfo, error) {
	if name == filesystem.Root {
		return &fsys.info, nil
	}
	if subsys := fsys.ipfs; subsys != nil {
		return statFn(subsys, name)
	}
	return nil, fserrors.New(op, name, fs.ErrNotExist, fserrors.NotExist)
}

func (fsys *FS) nodeContext() (context.Context, context.CancelFunc) {
	var (
		ctx     = fsys.ctx
		timeout = fsys.apiTimeout
	)
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func (fsys *FS) Symlink(oldname, newname string) error {
	const op = "symlink"
	fsys.cacheMu.Lock()
	defer fsys.cacheMu.Unlock()
	var (
		dag         = fsys.dag
		ctx, cancel = fsys.nodeContext()
	)
	defer cancel()
	if dag == nil {
		err := fmt.Errorf("%w - system created without dag service option",
			errors.ErrUnsupported,
		)
		return fserrors.New(op, newname, err, fserrors.InvalidOperation)
	}
	linkCid, err := intp.MakeAndAddLink(ctx, oldname, dag)
	if err != nil {
		return fserrors.New(op, newname, err, fserrors.IO)
	}
	path := corepath.IpfsPath(linkCid)
	if err := fsys.api.Add(ctx, path); err != nil {
		return fserrors.New(op, newname, err, fserrors.IO)
	}
	if cacheEnabled := fsys.expiry > 0; cacheEnabled {
		// We modified the pinset; invalidate the cache.
		fsys.info.modTime.Store(new(time.Time))
	}
	return nil
}

func (fsys *FS) ReadLink(name string) (string, error) {
	const op = "readlink"
	if name == filesystem.Root {
		const kind = fserrors.InvalidItem
		return "", fserrors.New(op, name, intp.ErrRootLink, kind)
	}
	if subsys := fsys.ipfs; subsys != nil {
		return filesystem.Readlink(subsys, name)
	}
	return "", fserrors.New(op, name, fs.ErrNotExist, fserrors.NotExist)
}

func (fsys *FS) Open(name string) (fs.File, error) {
	const op = "open"
	if name == filesystem.Root {
		return fsys.openRoot()
	}
	if subsys := fsys.ipfs; subsys != nil {
		return subsys.Open(name)
	}
	return nil, fserrors.New(op, name, fs.ErrNotExist, fserrors.NotExist)
}

func (fsys *FS) openRoot() (fs.ReadDirFile, error) {
	var (
		dirCtx, cancel = context.WithCancel(fsys.ctx)
		entries, err   = fsys.getEntries(dirCtx)
	)
	if err != nil {
		cancel()
		return nil, err
	}
	return &pinDirectory{
		pinShared: &fsys.pinShared,
		stream: &intp.EntryStream{
			Context: dirCtx, CancelFunc: cancel,
			Ch: entries,
		},
	}, nil
}

func (fsys *FS) getEntries(ctx context.Context) (<-chan filesystem.StreamDirEntry, error) {
	if cacheDisabled := fsys.expiry == 0; cacheDisabled {
		return fsys.fetchEntries(ctx)
	}
	fsys.cacheMu.Lock()
	if fsys.validLocked() {
		entries := fsys.generateEntriesLocked(ctx)
		fsys.cacheMu.Unlock()
		return entries, nil
	}
	return fsys.fetchAndCacheThenUnlock(ctx)
}

func (fsys *FS) validLocked() bool {
	var (
		expiry  = fsys.expiry
		forever = expiry < 0
	)
	if forever || time.Since(*fsys.info.modTime.Load()) < expiry {
		return true
	}
	return false
}

func (fsys *FS) generateEntriesLocked(ctx context.Context) <-chan filesystem.StreamDirEntry {
	var (
		snapshot = fsys.snapshot
		instance = make([]filesystem.StreamDirEntry, len(snapshot))
	)
	copy(instance, snapshot)
	return intp.GenerateEntryChan(ctx, instance)
}

func (fsys *FS) fetchEntries(ctx context.Context) (<-chan filesystem.StreamDirEntry, error) {
	var (
		api       = fsys.api
		pins, err = api.Ls(ctx, coreoptions.Pin.Ls.Recursive())
	)
	if err != nil {
		return nil, err
	}
	var (
		entries = intp.NewStreamChan(pins)
		statFn  = fsys.statFn
	)
	go func() {
		defer close(entries)
		for pin := range pins {
			entry := pinDirEntry{Pin: pin}
			if pin.Err() == nil {
				if err := statFn(&entry); err != nil {
					select {
					case entries <- intp.NewErrorEntry(err):
					case <-ctx.Done():
						intp.DrainThenSendErr(entries, ctx.Err())
					}
					return
				}
			}
			select {
			case entries <- &entry:
			case <-ctx.Done():
				intp.DrainThenSendErr(entries, ctx.Err())
				return
			}
		}
	}()
	return entries, nil
}

func (fsys *FS) fetchAndCacheThenUnlock(ctx context.Context) (<-chan filesystem.StreamDirEntry, error) {
	return intp.AccumulateAndRelay(
		fsys.ctx, ctx,
		fsys.fetchEntries, fsys.snapshot[:0],
		func(accumulator []filesystem.StreamDirEntry) {
			if accumulator != nil {
				fsys.snapshot = accumulator
				now := time.Now()
				fsys.info.modTime.Store(&now)
			}
			fsys.cacheMu.Unlock()
		})
}

func (fsys *FS) Close() error {
	fsys.cancel()
	return nil
}

func (*pinDirectoryInfo) Name() string          { return filesystem.Root }
func (*pinDirectoryInfo) Size() int64           { return 0 }
func (pi *pinDirectoryInfo) Mode() fs.FileMode  { return fs.ModeDir | pi.permissions }
func (pi *pinDirectoryInfo) ModTime() time.Time { return *pi.modTime.Load() }
func (pi *pinDirectoryInfo) IsDir() bool        { return pi.Mode().IsDir() }
func (pi *pinDirectoryInfo) Sys() any           { return pi }

func (pd *pinDirectory) Stat() (fs.FileInfo, error) { return &pd.info, nil }
func (*pinDirectory) Read([]byte) (int, error) {
	const op = "read"
	return -1, fserrors.New(op, filesystem.Root, filesystem.ErrIsDir, fserrors.IsDir)
}

func (pd *pinDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	const op = "readdir"
	if err := pd.err; err != nil {
		return nil, err
	}
	stream := pd.stream
	if stream == nil {
		return nil, fserrors.New(op, filesystem.Root, fs.ErrClosed, fserrors.Closed)
	}
	var (
		ctx       = stream.Context
		entryChan = stream.Ch
	)
	entries, err := intp.ReadEntries(ctx, entryChan, count)
	if err != nil {
		err = intp.ReaddirErr(op, filesystem.Root, err)
		pd.err = err
	}
	return entries, err
}

func (pd *pinDirectory) StreamDir() <-chan filesystem.StreamDirEntry {
	const op = "streamdir"
	if stream := pd.stream; stream != nil {
		return stream.Ch
	}
	errs := make(chan filesystem.StreamDirEntry, 1)
	errs <- intp.NewErrorEntry(
		fserrors.New(op, filesystem.Root, fs.ErrClosed, fserrors.Closed),
	)
	close(errs)
	return errs
}

func (pd *pinDirectory) Close() error {
	const op = "close"
	if stream := pd.stream; stream != nil {
		stream.CancelFunc()
		pd.stream = nil
		return nil
	}
	return fserrors.New(op, filesystem.Root, fs.ErrClosed, fserrors.Closed)
}

func (pe *pinDirEntry) Name() string {
	return pe.Pin.Path().Cid().String()
}

func (pe *pinDirEntry) Info() (fs.FileInfo, error) {
	return pe, nil
}

func (pe *pinDirEntry) Type() fs.FileMode {
	info, err := pe.Info()
	if err != nil {
		return fs.ModeIrregular
	}
	return info.Mode().Type()
}

func (pe *pinDirEntry) IsDir() bool        { return pe.Type().IsDir() }
func (pe *pinDirEntry) Error() error       { return pe.Pin.Err() }
func (pe *pinDirEntry) Size() int64        { return pe.size }
func (pe *pinDirEntry) Mode() fs.FileMode  { return pe.mode }
func (pe *pinDirEntry) ModTime() time.Time { return pe.modTime }
func (pe *pinDirEntry) Sys() any           { return pe }
