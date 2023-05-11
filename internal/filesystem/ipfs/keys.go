package ipfs

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	coreiface "github.com/ipfs/boxo/coreiface"
)

type (
	KeyFS struct {
		keyAPI      coreiface.KeyAPI
		ipns        fs.FS
		ctx         context.Context
		cancel      context.CancelFunc
		permissions fs.FileMode
	}
	KeyFSOption  func(*KeyFS) error
	keyDirectory struct {
		ipns   fs.FS
		stream *entryStream
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

const (
	KeyFSID       filesystem.ID = "KeyFS"
	keyfsRootName               = rootName
)

func WithIPNS(ipns fs.FS) KeyFSOption {
	return func(ka *KeyFS) error { ka.ipns = ipns; return nil }
}

func NewKeyFS(core coreiface.KeyAPI, options ...KeyFSOption) (*KeyFS, error) {
	fsys := &KeyFS{
		permissions: readAll | executeAll,
		keyAPI:      core,
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

func (ki *KeyFS) Close() error {
	ki.cancel()
	return nil
}

// TODO: probably inefficient. Review.
// TODO: deceptive name. This may translate the name.
// but it won't if we don't have such a key
// (which is fine for non-named IPNS paths).
func (ki *KeyFS) translateName(name string) (string, error) {
	keys, err := ki.keyAPI.List(ki.ctx)
	if err != nil {
		return "", err
	}
	var (
		components = strings.Split(name, "/")
		keyName    = components[0]
	)
	for _, key := range keys {
		if key.Name() == keyName {
			keyName = pathWithoutNamespace(key)
			break
		}
	}
	components = append([]string{keyName}, components[1:]...)
	keyName = strings.Join(components, "/")
	return keyName, nil
}

func (kfs *KeyFS) Open(name string) (fs.File, error) {
	const op = "open"
	if name == rootName {
		file, err := kfs.openRoot()
		if err != nil {
			return nil, err
		}
		return file, nil
	}
	translated, err := kfs.translateName(name)
	if err != nil {
		return nil, newFSError(op, name, err, fserrors.IO)
	}
	if subsys := kfs.ipns; subsys != nil {
		return subsys.Open(translated)
	}
	return nil, newFSError(op, name, ErrNotFound, fserrors.NotExist)
}

func (kfs *KeyFS) openRoot() (fs.ReadDirFile, error) {
	const (
		op      = "open"
		errKind = fserrors.IO
	)
	rootCtx := kfs.ctx
	if err := rootCtx.Err(); err != nil {
		return nil, newFSError(op, keyfsRootName, err, errKind)
	}
	var (
		dirCtx, dirCancel = context.WithCancel(rootCtx)
		entries           = make(chan filesystem.StreamDirEntry)
	)
	go func() {
		keys, err := kfs.keyAPI.List(dirCtx)
		if err != nil {
			dirCancel()
		}
		for _, key := range keys {
			entries <- &keyDirEntry{
				permissions: kfs.permissions,
				Key:         key,
				ipns:        kfs.ipns,
			}
		}
		close(entries)
	}()
	return &keyDirectory{
		mode: fs.ModeDir | kfs.permissions,
		ipns: kfs.ipns,
		stream: &entryStream{
			Context: dirCtx, CancelFunc: dirCancel,
			ch: entries,
		},
	}, nil
}

func (*keyDirectory) Read([]byte) (int, error) {
	const op = "keyDirectory.Read"
	return -1, newFSError(op, keyfsRootName, ErrIsDir, fserrors.IsDir)
}

func (kd *keyDirectory) Stat() (fs.FileInfo, error) { return kd, nil }

func (*keyDirectory) Name() string          { return rootName }
func (*keyDirectory) Size() int64           { return 0 }
func (kd *keyDirectory) Mode() fs.FileMode  { return kd.mode }
func (kd *keyDirectory) ModTime() time.Time { return time.Now() } // TODO: is there any way the node can tell us when the last publish was?
func (kd *keyDirectory) IsDir() bool        { return kd.Mode().IsDir() }
func (kd *keyDirectory) Sys() any           { return kd }

func (kd *keyDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	const op = "keyDirectory.ReadDir"
	stream := kd.stream
	if stream == nil {
		return nil, newFSError(op, keyfsRootName, ErrNotOpen, fserrors.IO)
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
		stream.ch = nil
		err = readdirErr(op, keyfsRootName, err)
	}
	return ents, err
}

func (kd *keyDirectory) Close() error {
	const op = "keyDirectory.Close"
	if stream := kd.stream; stream != nil {
		stream.CancelFunc()
		kd.stream = nil
		return nil
	}
	return newFSError(op, keyfsRootName, ErrNotOpen, fserrors.InvalidItem)
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
	return info.Mode() & fs.ModeType
}

func (ke *keyDirEntry) IsDir() bool { return ke.Type()&fs.ModeDir != 0 }
func (*keyDirEntry) Error() error   { return nil }

func (ki *keyInfo) Name() string       { return ki.name }
func (*keyInfo) Size() int64           { return 0 } // Unknown without IPNS subsystem.
func (ki *keyInfo) Mode() fs.FileMode  { return ki.mode }
func (ki *keyInfo) ModTime() time.Time { return time.Now() } // TODO: is there any way the node can tell us when the last publish was?
func (ki *keyInfo) IsDir() bool        { return ki.Mode().IsDir() }
func (ki *keyInfo) Sys() any           { return ki }
