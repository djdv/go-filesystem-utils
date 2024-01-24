package ipfs

import (
	"context"
	"io/fs"
	"sync"
	"sync/atomic"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	coreiface "github.com/ipfs/boxo/coreiface"
	coreoptions "github.com/ipfs/boxo/coreiface/options"
)

type (
	pinDirectoryInfo struct {
		modTime     atomic.Pointer[time.Time]
		permissions fs.FileMode
	}
	pinShared struct {
		api  coreiface.PinAPI
		ipfs fs.FS
		info pinDirectoryInfo
	}
	PinFS struct {
		ctx    context.Context
		cancel context.CancelFunc
		statFn func(*pinDirEntry) error
		pinShared
		snapshot []filesystem.StreamDirEntry
		expiry   time.Duration
		cacheMu  sync.RWMutex
	}
	pinDirectory struct {
		*pinShared
		stream *entryStream
		err    error
	}
	pinDirEntry struct {
		coreiface.Pin
		modTime time.Time
		mode    fs.FileMode
		size    int64
	}
	PinFSOption func(*PinFS) error
)

const PinFSID filesystem.ID = "PinFS"

func NewPinFS(pinAPI coreiface.PinAPI, options ...PinFSOption) (*PinFS, error) {
	fsys := PinFS{
		pinShared: pinShared{
			api: pinAPI,
			info: pinDirectoryInfo{
				permissions: readAll | executeAll,
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

// WithIPFS supplies an IPFS instance to
// use for added functionality.
// One such case is resolving a pin's file metadata.
func WithIPFS(ipfs fs.FS) PinFSOption {
	return func(pfs *PinFS) error { pfs.ipfs = ipfs; return nil }
}

func (pfs *PinFS) setContext(ctx context.Context) {
	pfs.ctx, pfs.cancel = context.WithCancel(ctx)
}

func (pfs *PinFS) setPermissions(permissions fs.FileMode) {
	pfs.info.permissions = permissions.Perm()
}

func (pfs *PinFS) initStatFunc() {
	var (
		ipfs        = pfs.ipfs
		permissions = &pfs.info.permissions
	)
	if ipfs == nil {
		pfs.statFn = func(entry *pinDirEntry) error {
			entry.mode = permissions.Perm()
			entry.modTime = time.Now()
			return nil
		}
		return
	}
	pfs.statFn = func(entry *pinDirEntry) error {
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

func CachePinsFor(duration time.Duration) PinFSOption {
	return func(pfs *PinFS) error {
		pfs.expiry = duration
		return nil
	}
}

func (*PinFS) ID() filesystem.ID { return PinFSID }

func (pfs *PinFS) Stat(name string) (fs.FileInfo, error) {
	const op = "stat"
	if name == filesystem.Root {
		return &pfs.info, nil
	}
	if subsys := pfs.ipfs; subsys != nil {
		return fs.Stat(subsys, name)
	}
	return nil, fserrors.New(op, name, fs.ErrNotExist, fserrors.NotExist)
}

func (pfs *PinFS) Open(name string) (fs.File, error) {
	const op = "open"
	if name == filesystem.Root {
		return pfs.openRoot()
	}
	if subsys := pfs.ipfs; subsys != nil {
		return subsys.Open(name)
	}
	return nil, fserrors.New(op, name, fs.ErrNotExist, fserrors.NotExist)
}

func (pfs *PinFS) openRoot() (fs.ReadDirFile, error) {
	var (
		dirCtx, cancel = context.WithCancel(pfs.ctx)
		entries, err   = pfs.getEntries(dirCtx)
	)
	if err != nil {
		cancel()
		return nil, err
	}
	return &pinDirectory{
		pinShared: &pfs.pinShared,
		stream: &entryStream{
			Context: dirCtx, CancelFunc: cancel,
			ch: entries,
		},
	}, nil
}

func (pfs *PinFS) getEntries(ctx context.Context) (<-chan filesystem.StreamDirEntry, error) {
	cacheDisabled := pfs.expiry == 0
	if cacheDisabled {
		return pfs.fetchEntries(ctx)
	}
	pfs.cacheMu.Lock()
	if pfs.validLocked() {
		entries := pfs.generateEntriesLocked(ctx)
		pfs.cacheMu.Unlock()
		return entries, nil
	}
	return pfs.fetchAndCacheThenUnlock(ctx)
}

func (pfs *PinFS) validLocked() bool {
	var (
		expiry  = pfs.expiry
		forever = expiry < 0
	)
	if forever || time.Since(*pfs.info.modTime.Load()) < expiry {
		return true
	}
	return false
}

func (pfs *PinFS) generateEntriesLocked(ctx context.Context) <-chan filesystem.StreamDirEntry {
	var (
		snapshot = pfs.snapshot
		instance = make([]filesystem.StreamDirEntry, len(snapshot))
		_        = copy(instance, snapshot)
	)
	return generateEntryChan(ctx, instance)
}

func (pfs *PinFS) fetchEntries(ctx context.Context) (<-chan filesystem.StreamDirEntry, error) {
	var (
		api       = pfs.api
		pins, err = api.Ls(ctx, coreoptions.Pin.Ls.Recursive())
	)
	if err != nil {
		return nil, err
	}
	var (
		entries = newStreamChan(pins)
		statFn  = pfs.statFn
	)
	go func() {
		defer close(entries)
		for pin := range pins {
			entry := pinDirEntry{Pin: pin}
			if pin.Err() == nil {
				if err := statFn(&entry); err != nil {
					select {
					case entries <- newErrorEntry(err):
					case <-ctx.Done():
						drainThenSendErr(entries, ctx.Err())
					}
					return
				}
			}
			select {
			case entries <- &entry:
			case <-ctx.Done():
				drainThenSendErr(entries, ctx.Err())
				return
			}
		}
	}()
	return entries, nil
}

func (pfs *PinFS) fetchAndCacheThenUnlock(ctx context.Context) (<-chan filesystem.StreamDirEntry, error) {
	fetchCtx, cancel := context.WithCancel(pfs.ctx)
	fetched, err := pfs.fetchEntries(fetchCtx)
	if err != nil {
		pfs.cacheMu.Unlock()
		cancel()
		return nil, err
	}
	var (
		relay       = newStreamChan(fetched)
		accumulator = pfs.snapshot[:0]
	)
	go func() {
		defer func() { cancel(); pfs.cacheMu.Unlock() }()
		sawError, snapshot := accumulateRelayClose(ctx, fetched, relay, accumulator)
		if sawError || fetchCtx.Err() != nil {
			// Time stamp remains expired.
			return // Caller must try to fetch again.
		}
		pfs.snapshot = generic.CompactSlice(snapshot)
		now := time.Now()
		pfs.info.modTime.Store(&now)
	}()
	return relay, nil
}

func (pfs *PinFS) Close() error {
	pfs.cancel()
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
		// TODO: We don't have an error kind
		// that translates into EBADF
		return nil, fserrors.New(op, filesystem.Root, fs.ErrClosed, fserrors.IO)
	}
	var (
		ctx       = stream.Context
		entryChan = stream.ch
	)
	entries, err := readEntries(ctx, entryChan, count)
	if err != nil {
		err = readdirErr(op, filesystem.Root, err)
		pd.err = err
	}
	return entries, err
}

func (pd *pinDirectory) StreamDir() <-chan filesystem.StreamDirEntry {
	const op = "streamdir"
	stream := pd.stream
	if stream == nil {
		errs := make(chan filesystem.StreamDirEntry, 1)
		// TODO: We don't have an error kind
		// that translates into EBADF
		errs <- newErrorEntry(
			fserrors.New(op, filesystem.Root, fs.ErrClosed, fserrors.IO),
		)
		return errs
	}
	return stream.ch
}

func (pd *pinDirectory) Close() error {
	const op = "close"
	if stream := pd.stream; stream != nil {
		stream.CancelFunc()
		pd.stream = nil
		return nil
	}
	// TODO: We don't have an error kind
	// that translates into EBADF
	return fserrors.New(op, filesystem.Root, fs.ErrClosed, fserrors.IO)
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
