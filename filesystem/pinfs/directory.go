package pinfs

import (
	"context"
	"io"
	"io/fs"
	"path"
	"sort"

	"github.com/djdv/go-filesystem-utils/filesystem/errors"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	coreoptions "github.com/ipfs/interface-go-ipfs-core/options"
)

type pinDirectory struct {
	ctx    context.Context
	cancel context.CancelFunc
	stat   *rootStat
	pinAPI coreiface.PinAPI
	pins   <-chan coreiface.Pin
	ipfs   fs.FS
}

type pinsByName []fs.DirEntry

func (pins pinsByName) Len() int           { return len(pins) }
func (pins pinsByName) Swap(i, j int)      { pins[i], pins[j] = pins[j], pins[i] }
func (pins pinsByName) Less(i, j int) bool { return pins[i].Name() < pins[j].Name() }

func (pd *pinDirectory) Stat() (fs.FileInfo, error) { return pd.stat, nil }

func (*pinDirectory) Read([]byte) (int, error) {
	const op errors.Op = "pinDirectory.Read"
	return -1, errors.New(op, errors.IsDir)
}

func (pd *pinDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	const op errors.Op = "pinDirectory.ReadDir"
	pins := pd.pins
	if pins == nil {
		pinChan, err := pd.pinAPI.Ls(pd.ctx, coreoptions.Pin.Ls.Recursive())
		if err != nil {
			return nil, errors.New(op,
				errors.IO,
				err,
			)
		}
		pins = pinChan
		pd.pins = pins
	}

	var (
		ents []fs.DirEntry
		err  error
	)
	if count <= 0 {
		// NOTE: [spec] This will cause the loop below to become infinite.
		// This is intended by the fs.FS spec
		count = -1
	} else {
		// If we're dealing with a finite amount, allocate for it.
		// NOTE: If the caller passes an unreasonably large `count`,
		// we do nothing to protect against OOM.
		// This is to be considered a client-side implementation error
		// and should be fixed caller side.
		ents = make([]fs.DirEntry, 0, count)
	}
	for ; count != 0; count-- {
		pin, ok := <-pins
		if !ok {
			if count > 0 {
				err = io.EOF
			}
			break
		}
		ents = append(ents, &pinDirEntry{Pin: pin, ipfs: pd.ipfs})
	}

	sort.Sort(pinsByName(ents))

	return ents, err
}

type pinDirEntry struct {
	coreiface.Pin
	ipfs fs.FS
}

func (pe *pinDirEntry) Name() string { return path.Base(pe.Path().String()) }

func (pe *pinDirEntry) Info() (fs.FileInfo, error) {
	return fs.Stat(pe.ipfs, pe.Pin.Path().Cid().String())
}

func (pe *pinDirEntry) Type() fs.FileMode {
	info, err := pe.Info()
	if err != nil {
		return fs.ModeIrregular
	}
	return info.Mode() & fs.ModeType
}

func (pe *pinDirEntry) IsDir() bool { return pe.Type()&fs.ModeDir != 0 }

func (pd *pinDirectory) Close() error {
	const op errors.Op = "pinfs.Close"
	cancel := pd.cancel
	pd.cancel = nil
	if cancel == nil {
		return errors.New(op,
			errors.InvalidItem, // TODO: Check POSIX expected values
			"directory was not open",
		)
	}
	cancel()
	return nil
}
