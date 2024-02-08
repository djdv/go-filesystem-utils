package ipfs

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
	KeyFS struct {
		keyAPI      coreiface.KeyAPI
		dag         coreiface.APIDagService
		names       coreiface.NameAPI
		pins        coreiface.PinAPI
		ipns        fs.FS
		ctx         context.Context
		cancel      context.CancelFunc
		info        nodeInfo
		nodeTimeout time.Duration
		cache       keyfsCacheEntry
		cacheMu     sync.Mutex
		linkLimit   uint
		expiry      time.Duration
	}
	KeyFSOption  func(*KeyFS) error
	keyDirectory struct {
		ipns   fs.FS
		stream *entryStream
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

const KeyFSID filesystem.ID = "KeyFS"

func WithIPNS(ipns fs.FS) KeyFSOption {
	return func(ka *KeyFS) error { ka.ipns = ipns; return nil }
}

func WithNameService(names coreiface.NameAPI) KeyFSOption {
	return func(ka *KeyFS) error { ka.names = names; return nil }
}

func WithPinService(pins coreiface.PinAPI) KeyFSOption {
	return func(ka *KeyFS) error { ka.pins = pins; return nil }
}

// CacheKeysFor will cache responses from the node and consider
// them valid for the duration. Negative values retain the
// cache forever. A 0 value disables caching.
func CacheKeysFor(duration time.Duration) KeyFSOption {
	return func(kfs *KeyFS) error {
		kfs.expiry = duration
		return nil
	}
}

func NewKeyFS(core coreiface.KeyAPI, options ...KeyFSOption) (*KeyFS, error) {
	const permissions = readAll | executeAll
	fsys := &KeyFS{
		info: nodeInfo{
			modTime: time.Now(),
			name:    filesystem.Root,
			mode:    fs.ModeDir | permissions,
		},
		keyAPI:    core,
		linkLimit: 40, // Arbitrary.
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

func (*KeyFS) ID() filesystem.ID { return KeyFSID }

func (fsys *KeyFS) setContext(ctx context.Context) {
	fsys.ctx, fsys.cancel = context.WithCancel(ctx)
}

func (fsys *KeyFS) setNodeTimeout(timeout time.Duration) {
	fsys.nodeTimeout = timeout
}

func (fsys *KeyFS) setLinkLimit(limit uint) {
	fsys.linkLimit = limit
}

func (fsys *KeyFS) setPermissions(permissions fs.FileMode) {
	typ := fsys.info.mode.Type()
	fsys.info.mode = typ | permissions.Perm()
}

func (kfs *KeyFS) setDag(dag coreiface.APIDagService) {
	kfs.dag = dag
}

func (ki *KeyFS) Close() error {
	ki.cancel()
	return nil
}

func (ki *KeyFS) nodeContext() (context.Context, context.CancelFunc) {
	var (
		ctx     = ki.ctx
		timeout = ki.nodeTimeout
	)
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func (ki *KeyFS) getKeys() ([]coreiface.Key, error) {
	expiry := ki.expiry
	if cacheDisabled := expiry == 0; cacheDisabled {
		return ki.fetchKeys()
	}
	ki.cacheMu.Lock()
	defer ki.cacheMu.Unlock()
	if cache := ki.cache; time.Since(cache.last) < expiry {
		return cache.snapshot, nil
	}
	keys, err := ki.fetchKeys()
	if err != nil {
		return nil, err
	}
	ki.cache = keyfsCacheEntry{
		snapshot: keys,
		last:     time.Now(),
	}
	return keys, nil
}

func (ki *KeyFS) fetchKeys() ([]coreiface.Key, error) {
	ctx, cancel := ki.nodeContext()
	defer cancel()
	return ki.keyAPI.List(ctx)
}

// maybeTranslateName will translate the first component
// of `name` if the component is the name of a key that we have.
// Otherwise `name` is returned unchanged.
func (ki *KeyFS) maybeTranslateName(name string) (string, error) {
	keys, err := ki.getKeys()
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

func (kfs *KeyFS) Lstat(name string) (fs.FileInfo, error) {
	const op = "lstat"
	return kfs.stat(op, name, filesystem.Lstat)
}

func (kfs *KeyFS) Stat(name string) (fs.FileInfo, error) {
	const op = "stat"
	return kfs.stat(op, name, filesystem.Lstat)
}

func (kfs *KeyFS) stat(op, name string, statFn statFunc) (fs.FileInfo, error) {
	if name == filesystem.Root {
		return &kfs.info, nil
	}
	ipns := kfs.ipns
	if ipns == nil {
		return nil, fserrors.New(op, name, fs.ErrNotExist, fserrors.NotExist)
	}
	translated, err := kfs.maybeTranslateName(name)
	if err != nil {
		return nil, fserrors.New(op, name, err, fserrors.IO)
	}
	return statFn(ipns, translated)
}

func (kfs *KeyFS) Symlink(oldname, newname string) error {
	const op = "symlink"
	dag, names, err := kfs.symlinkAPIs()
	if err != nil {
		return fserrors.New(op, newname, err, fserrors.InvalidOperation)
	}
	var (
		api         = kfs.keyAPI
		ctx, cancel = kfs.nodeContext()
	)
	defer cancel()
	key, err := api.Generate(ctx, newname)
	if err != nil {
		return fserrors.New(op, newname, err, fserrors.IO)
	}
	linkCid, err := makeAndAddLink(ctx, oldname, dag)
	if err != nil {
		return fserrors.New(op, newname, err, fserrors.IO)
	}
	path := corepath.IpfsPath(linkCid)
	if pins := kfs.pins; pins != nil {
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

func (kfs *KeyFS) symlinkAPIs() (coreiface.APIDagService, coreiface.NameAPI, error) {
	var (
		dag   = kfs.dag
		names = kfs.names
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

func (kfs *KeyFS) Readlink(name string) (string, error) {
	const op = "readlink"
	if name == filesystem.Root {
		const kind = fserrors.InvalidItem
		return "", fserrors.New(op, name, errRootLink, kind)
	}
	if subsys := kfs.ipns; subsys != nil {
		return filesystem.Readlink(subsys, name)
	}
	return "", fserrors.New(op, name, fs.ErrNotExist, fserrors.NotExist)
}

func (kfs *KeyFS) Open(name string) (fs.File, error) {
	const depth = 0
	return kfs.open(name, depth)
}

func (kfs *KeyFS) open(name string, depth uint) (fs.File, error) {
	const op = "open"
	if name == filesystem.Root {
		return kfs.openRoot()
	}
	if err := validatePath(op, name); err != nil {
		return nil, err
	}
	ipns := kfs.ipns
	if ipns == nil {
		return nil, fserrors.New(op, name, fs.ErrNotExist, fserrors.NotExist)
	}
	translated, err := kfs.maybeTranslateName(name)
	if err != nil {
		return nil, fserrors.New(op, name, err, fserrors.IO)
	}
	info, err := kfs.Lstat(translated)
	if err != nil {
		return nil, err
	}
	if info.Mode().Type() == fs.ModeSymlink {
		if depth++; depth >= kfs.linkLimit {
			return nil, linkLimitError(op, name, kfs.linkLimit)
		}
		target, err := filesystem.Readlink(ipns, translated)
		if err != nil {
			return nil, err
		}
		return kfs.open(target, depth)
	}
	return ipns.Open(translated)
}

func (kfs *KeyFS) openRoot() (fs.ReadDirFile, error) {
	const (
		op      = "open"
		errKind = fserrors.IO
	)
	rootCtx := kfs.ctx
	if err := rootCtx.Err(); err != nil {
		return nil, fserrors.New(op, filesystem.Root, err, errKind)
	}
	var (
		dirCtx, dirCancel = context.WithCancel(rootCtx)
		entries           = make(chan filesystem.StreamDirEntry, 1)
		permissions       = kfs.info.mode.Perm()
	)
	go func() {
		defer close(entries)
		keys, err := kfs.getKeys()
		if err != nil {
			dirCancel()
			entries <- errorEntry{error: err}
			return
		}
		for _, key := range keys {
			select {
			case entries <- &keyDirEntry{
				permissions: permissions,
				Key:         key,
				ipns:        kfs.ipns,
			}:
			case <-dirCtx.Done():
				return
			}
		}
	}()
	return &keyDirectory{
		mode: fs.ModeDir | permissions,
		ipns: kfs.ipns,
		stream: &entryStream{
			Context: dirCtx, CancelFunc: dirCancel,
			ch: entries,
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
		entries = stream.ch
	)
	if entries == nil {
		return nil, io.EOF
	}
	ents, err := readEntries(ctx, entries, count)
	if err != nil {
		err = readdirErr(op, filesystem.Root, err)
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

func (ki *keyInfo) Name() string       { return ki.name }
func (*keyInfo) Size() int64           { return 0 } // Unknown without IPNS subsystem.
func (ki *keyInfo) Mode() fs.FileMode  { return ki.mode }
func (ki *keyInfo) ModTime() time.Time { return time.Now() } // TODO: is there any way the node can tell us when the last publish was?
func (ki *keyInfo) IsDir() bool        { return ki.Mode().IsDir() }
func (ki *keyInfo) Sys() any           { return ki }
