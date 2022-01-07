package keyfs

import (
	"context"
	"io/fs"
	"path"

	"github.com/djdv/go-filesystem-utils/filesystem/errors"
	gofs "github.com/djdv/go-filesystem-utils/filesystem/go"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

// TODO: we should probablt split sequential directories from stream variants
// one can embed the other - stream should be lighter and preferred (like the original was)
type keyDirectory struct {
	ctx    context.Context
	cancel context.CancelFunc
	stat   *rootStat
	ipns   fs.FS
	keyAPI coreiface.KeyAPI
	keys   []coreiface.Key
}

func (kd *keyDirectory) Stat() (fs.FileInfo, error) { return kd.stat, nil }

func (*keyDirectory) Read([]byte) (int, error) {
	const op errors.Op = "keyDirectory.Read"
	return -1, errors.New(op, errors.IsDir)
}

func (kd *keyDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	const op errors.Op = "keyDirectory.ReadDir"
	keys := kd.keys
	if keys == nil {
		ks, err := kd.keyAPI.List(kd.ctx)
		if err != nil {
			return nil, errors.New(op, err)
		}
		keys = ks
		kd.keys = keys
	}

	entries := make(chan fs.DirEntry, len(keys))
	go func() {
		defer close(entries)
		for _, key := range keys {
			select {
			case entries <- &keyDirEntry{
				Key:  key,
				ipns: kd.ipns,
			}:
			case <-kd.ctx.Done():
				return
			}
		}
	}()
	return gofs.ReadDir(count, entries)
}

type keyDirEntry struct {
	coreiface.Key
	ipns fs.FS
}

func (ke *keyDirEntry) Name() string { return path.Base(ke.Key.Name()) }

func (ke *keyDirEntry) Info() (fs.FileInfo, error) {
	// TODO: do this more properly; we need to switch on the namespace
	// and direct to potentially non-ipns resolvers (like mfs if the name is a key we own).
	// Not just strip and relay.
	return fs.Stat(ke.ipns, pathWithoutNamespace(ke.Key))
}

func (ke *keyDirEntry) Type() fs.FileMode {
	info, err := ke.Info()
	if err != nil {
		return fs.ModeIrregular
	}
	return info.Mode() & fs.ModeType
}

func (ke *keyDirEntry) IsDir() bool { return ke.Type()&fs.ModeDir != 0 }

func (kd *keyDirectory) Close() error {
	const op errors.Op = "keyDirectory.Close"
	cancel := kd.cancel
	kd.cancel = nil
	if cancel == nil {
		return errors.New(op,
			errors.InvalidItem, // TODO: Check POSIX expected values
			"directory was not open",
		)
	}
	cancel()
	return nil
}
