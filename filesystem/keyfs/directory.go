package keyfs

import (
	"context"
	"io/fs"
	"path"
	"sort"

	"github.com/djdv/go-filesystem-utils/filesystem/errors"
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

type keysByName []fs.DirEntry

func (keys keysByName) Len() int           { return len(keys) }
func (keys keysByName) Swap(i, j int)      { keys[i], keys[j] = keys[j], keys[i] }
func (keys keysByName) Less(i, j int) bool { return keys[i].Name() < keys[j].Name() }

func (kd *keyDirectory) Stat() (fs.FileInfo, error) { return kd.stat, nil }

func (*keyDirectory) Read([]byte) (int, error) {
	const op errors.Op = "keyDirectory.Read"
	return -1, errors.New(op, errors.IsDir)
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func (kd *keyDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	const op errors.Op = "keyDirectory.ReadDir"
	entries := kd.keys
	if entries == nil {
		keys, err := kd.keyAPI.List(kd.ctx)
		if err != nil {
			return nil, err
		}
		entries = keys
		kd.keys = entries
	}

	var ents []fs.DirEntry
	if count <= 0 {
		// NOTE: [spec] This will cause the loop below to become infinite.
		// This is intended by the fs.FS spec
		count = -1
		ents = make([]fs.DirEntry, 0, len(entries))
	} else {
		// If we're dealing with a finite amount, allocate for it.
		// NOTE: If the caller passes an unreasonably large `count`,
		// we do nothing to protect against OOM.
		// This is to be considered a client-side implementation error
		// and should be fixed caller side.
		ents = make([]fs.DirEntry, 0, count)
	}

	for _, key := range entries {
		if count == 0 {
			break
		}
		ents = append(ents, &keyDirEntry{Key: key, ipns: kd.ipns})
		count--
	}
	kd.keys = entries[len(ents):]

	sort.Sort(keysByName(ents))

	return ents, nil
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
