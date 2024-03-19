package keyfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	intp "github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/internal"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	coreiface "github.com/ipfs/boxo/coreiface"
	coreoptions "github.com/ipfs/boxo/coreiface/options"
	corepath "github.com/ipfs/boxo/coreiface/path"
)

type (
	keyfsCacheEntry struct {
		last     time.Time
		snapshot []coreiface.Key
	}
	// FS implements [fs.FS] and [filesystem] extensions.
	FS struct {
		keyAPI     coreiface.KeyAPI
		dag        coreiface.APIDagService
		names      coreiface.NameAPI
		pins       coreiface.PinAPI
		ipns       fs.FS
		ctx        context.Context
		cancel     context.CancelFunc
		cache      keyfsCacheEntry
		info       intp.NodeInfo
		apiTimeout time.Duration
		linkLimit  uint
		expiry     time.Duration
		cacheMu    sync.Mutex
	}
	keyDirectory struct {
		ipns   fs.FS
		stream *intp.EntryStream
		err    error
		mode   fs.FileMode
	}
	keyDirEntry struct {
		coreiface.Key
		ipns        fs.FS
		permissions fs.FileMode
	}
	keyInfo struct { // TODO: roll into keyDirEntry?
		name string
		mode fs.FileMode // Without the type, this is only really useful for move+delete permissions.
	}
)

// ID defines the identifier of this system.
const ID filesystem.ID = "KeyFS"

// New constructs an [FS] using the defaults listed in the pkg constants.
// A list of [Option] values can be provided to change these defaults as desired.
func New(core coreiface.KeyAPI, options ...Option) (*FS, error) {
	fsys := &FS{
		info: intp.NodeInfo{
			ModTime_: time.Now(),
			Name_:    filesystem.Root,
			Mode_:    fs.ModeDir | DefaultPermissions,
		},
		keyAPI:     core,
		apiTimeout: DefaultAPITimeout,
		linkLimit:  DefaultLinkLimit,
		expiry:     DefaultCacheExpiry,
	}
	for _, setter := range options {
		if err := setter(fsys); err != nil {
			return nil, err
		}
	}
	if fsys.ctx == nil {
		fsys.ctx, fsys.cancel = context.WithCancel(context.Background())
	}
	return fsys, nil
}

func (*FS) ID() filesystem.ID { return ID }

func (fsys *FS) Close() error {
	fsys.cancel()
	return nil
}

func (fsys *FS) nodeContext() (context.Context, context.CancelFunc) {
	ctx := fsys.ctx
	if timeout := fsys.apiTimeout; timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return context.WithCancel(ctx)
}

func (fsys *FS) getKeys() ([]coreiface.Key, error) {
	expiry := fsys.expiry
	if cacheDisabled := expiry == 0; cacheDisabled {
		return fsys.fetchKeys()
	}
	fsys.cacheMu.Lock()
	defer fsys.cacheMu.Unlock()
	if cache := fsys.cache; time.Since(cache.last) < expiry {
		return cache.snapshot, nil
	}
	keys, err := fsys.fetchKeys()
	if err != nil {
		return nil, err
	}
	fsys.cache = keyfsCacheEntry{
		snapshot: keys,
		last:     time.Now(),
	}
	return keys, nil
}

func (fsys *FS) fetchKeys() ([]coreiface.Key, error) {
	ctx, cancel := fsys.nodeContext()
	defer cancel()
	return fsys.keyAPI.List(ctx)
}

// maybeTranslateName will translate the first component
// of `name` if the component is the name of a key that we have.
// Otherwise `name` is returned unchanged.
func (fsys *FS) maybeTranslateName(name string) (string, error) {
	keys, err := fsys.getKeys()
	if err != nil {
		return "", err
	}
	const separator = "/"
	var (
		components = strings.Split(name, separator)
		keyName    = components[0]
	)
	for _, key := range keys {
		if key.Name() != keyName {
			continue
		}
		tail := components[1:]
		components = append(
			[]string{pathWithoutNamespace(key)},
			tail...,
		)
		return strings.Join(components, separator), nil
	}
	return name, nil
}

func (fsys *FS) Lstat(name string) (fs.FileInfo, error) {
	const op = "lstat"
	return fsys.stat(op, name, filesystem.Lstat)
}

func (fsys *FS) Stat(name string) (fs.FileInfo, error) {
	const op = "stat"
	return fsys.stat(op, name, filesystem.Lstat)
}

func (fsys *FS) stat(op, name string, statFn intp.StatFunc) (fs.FileInfo, error) {
	if name == filesystem.Root {
		return &fsys.info, nil
	}
	ipns := fsys.ipns
	if ipns == nil {
		return nil, fserrors.New(op, name, fs.ErrNotExist, fserrors.NotExist)
	}
	translated, err := fsys.maybeTranslateName(name)
	if err != nil {
		return nil, fserrors.New(op, name, err, fserrors.IO)
	}
	return statFn(ipns, translated)
}

func (fsys *FS) Symlink(oldname, newname string) error {
	const op = "symlink"
	dag, names, err := fsys.symlinkAPIs()
	if err != nil {
		return fserrors.New(op, newname, err, fserrors.InvalidOperation)
	}
	var (
		api         = fsys.keyAPI
		ctx, cancel = fsys.nodeContext()
	)
	defer cancel()
	key, err := api.Generate(ctx, newname)
	if err != nil {
		return fserrors.New(op, newname, err, fserrors.IO)
	}
	linkCid, err := intp.MakeAndAddLink(ctx, oldname, dag)
	if err != nil {
		return fserrors.New(op, newname, err, fserrors.IO)
	}
	path := corepath.IpfsPath(linkCid)
	if pins := fsys.pins; pins != nil {
		if err := pins.Add(ctx, path); err != nil {
			return fserrors.New(op, newname, err, fserrors.IO)
		}
	}
	if _, err := names.Publish(ctx, path,
		coreoptions.Name.Key(key.Name()),
		coreoptions.Name.AllowOffline(true),
	); err != nil {
		return fserrors.New(op, newname, err, fserrors.IO)
	}
	return nil
}

func (fsys *FS) symlinkAPIs() (coreiface.APIDagService, coreiface.NameAPI, error) {
	var (
		dag   = fsys.dag
		names = fsys.names
		errs  []error
	)
	if dag == nil {
		const err = generic.ConstError("system created without dag service option")
		errs = append(errs, err)
	}
	if names == nil {
		const err = generic.ConstError("system created without name service option")
		errs = append(errs, err)
	}
	if errs == nil {
		return dag, names, nil
	}
	errs = append([]error{errors.ErrUnsupported}, errs...)
	return nil, nil, errors.Join(errs...)
}

func (fsys *FS) ReadLink(name string) (string, error) {
	const op = "readlink"
	if name == filesystem.Root {
		const kind = fserrors.InvalidItem
		return "", fserrors.New(op, name, intp.ErrRootLink, kind)
	}
	if subsys := fsys.ipns; subsys != nil {
		return filesystem.Readlink(subsys, name)
	}
	return "", fserrors.New(op, name, fs.ErrNotExist, fserrors.NotExist)
}

func (fsys *FS) Open(name string) (fs.File, error) {
	const depth = 0
	return fsys.open(name, depth)
}

func (fsys *FS) open(name string, depth uint) (fs.File, error) {
	const op = "open"
	if name == filesystem.Root {
		return fsys.openRoot()
	}
	if err := intp.ValidatePath(op, name); err != nil {
		return nil, err
	}
	ipns := fsys.ipns
	if ipns == nil {
		return nil, fserrors.New(op, name, fs.ErrNotExist, fserrors.NotExist)
	}
	translated, err := fsys.maybeTranslateName(name)
	if err != nil {
		return nil, fserrors.New(op, name, err, fserrors.IO)
	}
	info, err := fsys.Lstat(translated)
	if err != nil {
		return nil, err
	}
	if info.Mode().Type() == fs.ModeSymlink {
		if depth++; depth >= fsys.linkLimit {
			return nil, intp.LinkLimitError(op, name, fsys.linkLimit)
		}
		target, err := filesystem.Readlink(ipns, translated)
		if err != nil {
			return nil, err
		}
		return fsys.open(target, depth)
	}
	return ipns.Open(translated)
}

func (fsys *FS) openRoot() (fs.ReadDirFile, error) {
	const (
		op      = "open"
		errKind = fserrors.IO
	)
	rootCtx := fsys.ctx
	if err := rootCtx.Err(); err != nil {
		return nil, fserrors.New(op, filesystem.Root, err, errKind)
	}
	var (
		dirCtx, dirCancel = context.WithCancel(rootCtx)
		entries           = make(chan filesystem.StreamDirEntry, 1)
		permissions       = fsys.info.Mode_.Perm()
	)
	go func() {
		defer close(entries)
		keys, err := fsys.getKeys()
		if err != nil {
			dirCancel()
			entries <- intp.NewErrorEntry(err)
			return
		}
		for _, key := range keys {
			select {
			case entries <- &keyDirEntry{
				permissions: permissions,
				Key:         key,
				ipns:        fsys.ipns,
			}:
			case <-dirCtx.Done():
				return
			}
		}
	}()
	return &keyDirectory{
		mode: fs.ModeDir | permissions,
		ipns: fsys.ipns,
		stream: &intp.EntryStream{
			Context: dirCtx, CancelFunc: dirCancel,
			Ch: entries,
		},
	}, nil
}

func (*keyDirectory) Read([]byte) (int, error) {
	const op = "read"
	return -1, fserrors.New(op, filesystem.Root, filesystem.ErrIsDir, fserrors.IsDir)
}

func (kd *keyDirectory) Stat() (fs.FileInfo, error) { return kd, nil }

func (*keyDirectory) Name() string          { return filesystem.Root }
func (*keyDirectory) Size() int64           { return 0 }
func (kd *keyDirectory) Mode() fs.FileMode  { return kd.mode }
func (kd *keyDirectory) ModTime() time.Time { return time.Now() } // TODO: is there any way the node can tell us when the last publish was?
func (kd *keyDirectory) IsDir() bool        { return kd.Mode().IsDir() }
func (kd *keyDirectory) Sys() any           { return kd }

func (kd *keyDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	const op = "readdir"
	if err := kd.err; err != nil {
		return nil, err
	}
	stream := kd.stream
	if stream == nil {
		return nil, fserrors.New(op, filesystem.Root, fs.ErrClosed, fserrors.Closed)
	}
	var (
		ctx     = stream.Context
		entries = stream.Ch
	)
	if entries == nil {
		return nil, io.EOF
	}
	ents, err := intp.ReadEntries(ctx, entries, count)
	if err != nil {
		err = intp.ReaddirErr(op, filesystem.Root, err)
		kd.err = err
	}
	return ents, err
}

func (kd *keyDirectory) Close() error {
	const op = "close"
	if stream := kd.stream; stream != nil {
		stream.CancelFunc()
		kd.stream = nil
		return nil
	}
	return fserrors.New(op, filesystem.Root, fs.ErrClosed, fserrors.Closed)
}

func pathWithoutNamespace(key coreiface.Key) string {
	var (
		keyPath = key.Path()
		prefix  = fmt.Sprintf("/%s/", keyPath.Namespace())
	)
	return strings.TrimPrefix(keyPath.String(), prefix)
}

func (ke *keyDirEntry) Name() string { return path.Base(ke.Key.Name()) }

func (ke *keyDirEntry) Info() (fs.FileInfo, error) {
	if subsys := ke.ipns; subsys != nil {
		return fs.Stat(subsys, pathWithoutNamespace(ke.Key))
	}
	return &keyInfo{
		name: ke.Key.Name(),
		mode: fs.ModeIrregular | ke.permissions,
	}, nil
}

func (ke *keyDirEntry) Type() fs.FileMode {
	info, err := ke.Info()
	if err != nil {
		return fs.ModeIrregular
	}
	return info.Mode().Type()
}

func (ke *keyDirEntry) IsDir() bool { return ke.Type()&fs.ModeDir != 0 }
func (*keyDirEntry) Error() error   { return nil }

func (ki *keyInfo) Name() string      { return ki.name }
func (*keyInfo) Size() int64          { return 0 } // Unknown without IPNS subsystem.
func (ki *keyInfo) Mode() fs.FileMode { return ki.mode }
func (*keyInfo) ModTime() time.Time   { return time.Now() } // TODO: is there any way the node can tell us when the last publish was?
func (ki *keyInfo) IsDir() bool       { return ki.Mode().IsDir() }
func (ki *keyInfo) Sys() any          { return ki }
