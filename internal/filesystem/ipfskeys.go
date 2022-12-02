package filesystem

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

type (
	IPFSKeyFS struct {
		keyAPI coreiface.KeyAPI
		ipns   fs.FS
	}
	keyDirectory struct {
		mode    fs.FileMode
		ipns    fs.FS
		cancel  context.CancelFunc
		getKeys func() ([]coreiface.Key, error)
		cursor  int
	}
	keyDirEntry struct {
		permissions fs.FileMode
		coreiface.Key
		ipns fs.FS
	}
	keyInfo struct { // TODO: roll into keyDirEntry?
		name string
		mode fs.FileMode // Without the type, this is only really useful for move+delete permissions.
	}
)

func NewKeyFS(core coreiface.KeyAPI, options ...KeyfsOption) *IPFSKeyFS {
	fs := &IPFSKeyFS{keyAPI: core}
	for _, setter := range options {
		if err := setter(fs); err != nil {
			panic(err)
		}
	}
	return fs
}

func (*IPFSKeyFS) ID() ID       { return IPFSKeys }
func (*IPFSKeyFS) Close() error { return nil } // TODO: close everything

// TODO: probably inefficient. Review.
func (ki *IPFSKeyFS) translateName(name string) (string, error) {
	keys, err := ki.keyAPI.List(context.Background())
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

func (kfs *IPFSKeyFS) Open(name string) (fs.File, error) {
	const op = "open"
	if name == rootName {
		return kfs.openRoot()
	}
	translated, err := kfs.translateName(name)
	if err != nil {
		return nil,
			&fs.PathError{
				Op:   op,
				Path: name,
				Err:  fserrors.New(fserrors.InvalidItem), // TODO: convert old-style errors.
			}
	}
	if subsys := kfs.ipns; subsys != nil {
		return subsys.Open(translated)
	}
	return nil, &fs.PathError{
		Op:   op,
		Path: name,
		Err:  fserrors.New(fserrors.NotExist), // TODO old-style err
	}
}

func (kfs *IPFSKeyFS) openRoot() (fs.ReadDirFile, error) {
	var (
		ctx, cancel = context.WithCancel(context.Background())
		keys        []coreiface.Key
		lazyKeys    = func() ([]coreiface.Key, error) {
			if keys != nil {
				return keys, nil
			}
			var err error
			keys, err = kfs.keyAPI.List(ctx)
			return keys, err
		}
	)
	const permissions = readAll | executeAll // TODO: from ctor; writes will be valid eventually.
	return &keyDirectory{
		mode:    fs.ModeDir | permissions,
		ipns:    kfs.ipns,
		cancel:  cancel,
		getKeys: lazyKeys,
	}, nil
}

func (*keyDirectory) Read([]byte) (int, error) {
	const op fserrors.Op = "keyDirectory.Read"
	return -1, fserrors.New(op, fserrors.IsDir)
}

func (kd *keyDirectory) Stat() (fs.FileInfo, error) { return kd, nil }

func (*keyDirectory) Name() string          { return rootName }
func (*keyDirectory) Size() int64           { return 0 }
func (kd *keyDirectory) Mode() fs.FileMode  { return kd.mode }
func (kd *keyDirectory) ModTime() time.Time { return time.Now() } // TODO: is there any way the node can tell us when the last publish was?
func (kd *keyDirectory) IsDir() bool        { return kd.Mode().IsDir() }
func (kd *keyDirectory) Sys() any           { return kd }

func (kd *keyDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	const op fserrors.Op = "keyDirectory.ReadDir"
	var (
		keys, err = kd.getKeys()
		keyCount  = len(keys)
	)
	if err != nil {
		return nil, fserrors.New(op, err)
	}
	cursor := kd.cursor
	if cursor >= keyCount {
		return nil, io.EOF
	}

	keys = keys[cursor:]
	keyCount = len(keys)
	ents := make([]fs.DirEntry, 0, generic.Max(count, keyCount))
	if count == 0 {
		count-- // Intentionally bypass break condition / append all ents.
	}
	for _, key := range keys {
		if count == 0 {
			break
		}
		ents = append(ents, &keyDirEntry{
			permissions: kd.mode.Perm(),
			Key:         key,
			ipns:        kd.ipns,
		})
		count--
	}
	if count > 0 {
		return ents, io.EOF
	}
	return ents, nil
}

func (kd *keyDirectory) Close() error {
	const op fserrors.Op = "keyDirectory.Close"
	if cancel := kd.cancel; cancel != nil {
		cancel()
		return nil
	}
	return fserrors.New(op,
		fserrors.InvalidItem, // TODO: Check POSIX expected values
		"directory was not open",
	)
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

func (ki *keyInfo) Name() string       { return ki.name }
func (*keyInfo) Size() int64           { return 0 } // Unknown without IPNS subsystem.
func (ki *keyInfo) Mode() fs.FileMode  { return ki.mode }
func (ki *keyInfo) ModTime() time.Time { return time.Now() } // TODO: is there any way the node can tell us when the last publish was?
func (ki *keyInfo) IsDir() bool        { return ki.Mode().IsDir() }
func (ki *keyInfo) Sys() any           { return ki }
