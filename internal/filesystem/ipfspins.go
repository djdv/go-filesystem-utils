package filesystem

import (
	"context"
	"io"
	"io/fs"
	"sort"
	"time"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	coreoptions "github.com/ipfs/interface-go-ipfs-core/options"
)

type (
	IPFSPinAPI struct {
		pinAPI coreiface.PinAPI
		ipfs   FS // TODO: subsys should be handled via `bind` instead? fs.Subsys?
	}

	pinsDirectory struct {
		stat   fs.FileInfo
		ipfs   fs.FS
		pinAPI coreiface.PinAPI
		ents   <-chan []fs.DirEntry
	}

	pinDirEntry struct {
		coreiface.Pin
		ipfs fs.FS
	}
)

func NewPinFS(pinAPI coreiface.PinAPI, options ...PinfsOption) *IPFSPinAPI {
	fs := &IPFSPinAPI{pinAPI: pinAPI}
	for _, setter := range options {
		if err := setter(fs); err != nil {
			panic(err)
		}
	}
	return fs
}

func (*IPFSPinAPI) ID() ID { return IPFSPins }

func (pfs *IPFSPinAPI) Open(name string) (fs.File, error) {
	const op = "open"
	if name == rootName {
		return pfs.OpenDir(name)
	}
	if !fs.ValidPath(name) {
		return nil,
			&fs.PathError{
				Op:   op,
				Path: name,
				Err:  fserrors.New(fserrors.InvalidItem), // TODO: convert old-style errors.
			}
	}

	if subsys := pfs.ipfs; subsys != nil {
		return subsys.Open(name)
	}
	// TODO: stub pin-file here that can at least Stat itself.
	// As-is, ReadDir returns good ents,
	// but they can't be opened+stat'd, or fs.Stat'd
	// Either needs to be implemented.
	// Probably the latter.

	return nil, &fs.PathError{
		Op:   op,
		Path: name,
		Err:  fserrors.New(fserrors.NotExist), // TODO old-style err
	}
}

func (pfs *IPFSPinAPI) OpenDir(name string) (fs.ReadDirFile, error) {
	if name != rootName {
		if subsys := pfs.ipfs; subsys != nil {
			return subsys.OpenDir(name)
		}
		return nil, &fs.PathError{
			Op:   "open", // TODO: what does the fs.FS spec say for extensions? `opendir`?
			Path: name,
			Err:  fserrors.New(fserrors.NotExist), // TODO old-style err; convert to wrapped, defined, const errs.
		}
	}
	const op fserrors.Op = "pinfs.OpenDir"
	ctx := context.TODO() // TODO: cancel on close.
	pinEnts, err := getPinSliceChan(ctx, pfs.pinAPI, pfs.ipfs)
	if err != nil {
		err := fserrors.New(op,
			fserrors.IO,
			err,
		)
		return nil, err
	}
	pinDir := pfs.makePinsDir(s_IRXA)
	pinDir.ents = pinEnts
	return pinDir, nil
}

func (pfs *IPFSPinAPI) makePinsDir(permissions fs.FileMode) *pinsDirectory {
	return &pinsDirectory{
		stat: staticStat{
			name:    rootName,
			mode:    fs.ModeDir | permissions,
			modTime: time.Now(), // Not really modified, but pin-set as-of right now.
		},
		ipfs:   pfs.ipfs,
		pinAPI: pfs.pinAPI,
	}
}

func getPinSliceChan(ctx context.Context,
	pinAPI coreiface.PinAPI, ipfs FS,
) (<-chan []fs.DirEntry, error) {
	pins, err := getPinEntries(ctx, pinAPI)
	if err != nil {
		return nil, err
	}
	entSlices := make(chan []fs.DirEntry, 1)
	go func() {
		defer close(entSlices)
		ents, _ := pinsToDirEnts(ipfs, pins)
		// TODO: We need to do something about the errors here.
		// We could log them, or do this synchronously before OpenDir returns.
		// Or pass them to ReadDir somehow.
		entSlices <- ents
	}()
	return entSlices, nil
}

func getPinEntries(ctx context.Context, pinAPI coreiface.PinAPI) (<-chan coreiface.Pin, error) {
	lsOpts := []coreoptions.PinLsOption{
		coreoptions.Pin.Ls.Recursive(),
	}
	return pinAPI.Ls(ctx, lsOpts...)
}

func pinsToDirEnts(ipfs fs.FS, pins <-chan coreiface.Pin) ([]fs.DirEntry, error) {
	ents := make([]fs.DirEntry, 0, cap(pins))
	for pin := range pins {
		if pin.Err() != nil {
			return nil, pin.Err()
		}
		ents = append(ents, &pinDirEntry{Pin: pin, ipfs: ipfs})
	}
	sort.Sort(entsByName(ents))
	return ents, nil
}

func (pd *pinsDirectory) Stat() (fs.FileInfo, error) { return pd.stat, nil }

func (*pinsDirectory) Read([]byte) (int, error) {
	const op fserrors.Op = "pinDirectory.Read"
	return -1, fserrors.New(op, fserrors.IsDir)
}

func (pd *pinsDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	const op fserrors.Op = "pinDirectory.ReadDir"
	entsCh := pd.ents
	if entsCh == nil {
		return nil, fserrors.New(op, fserrors.IO) // TODO: error value for E-not-open?
	}
	ents := make([]fs.DirEntry, 0, generic.Max(count, 64)) // TODO: arbitrary cap
	if count == 0 {
		count-- // Intentionally bypass break condition / append all ents.
	}
	// TODO: entsCh should return a pair of channels or 2 values; {pin-converted-dir:pin-ls-err}
	for _, ent := range <-entsCh {
		if count == 0 {
			break
		}
		ents = append(ents, ent)
		count--
	}
	if count > 0 {
		return ents, io.EOF
	}
	return ents, nil
}

func (pd *pinsDirectory) Close() error {
	const op fserrors.Op = "pinfs.Close"
	if pd.ents != nil {
		pd.ents = nil
		return nil
	}
	return fserrors.New(op,
		fserrors.InvalidItem, // TODO: Check POSIX expected values
		"directory was not open",
	)
}

func (pe *pinDirEntry) Name() string {
	pinCid := pe.Pin.Path().Cid()
	if pinCid.Version() == 0 {
		pinCid = upgradeCid(pinCid)
	}
	return pinCid.String()
	// return path.Base(pe.Pin.Path().String())
}

func (pe *pinDirEntry) Info() (fs.FileInfo, error) {
	pinCid := pe.Pin.Path().Cid()
	if pinCid.Version() == 0 {
		pinCid = upgradeCid(pinCid)
	}

	if ipfs := pe.ipfs; ipfs != nil {
		return fs.Stat(pe.ipfs, pinCid.String())
	}
	return staticStat{
		name: pinCid.String(),
		// mode:    s_IRXA,
		mode:    fs.ModeDir | s_IRWXA,
		modTime: time.Now(),
	}, nil
}

func (pe *pinDirEntry) Type() fs.FileMode {
	info, err := pe.Info()
	if err != nil {
		return fs.ModeIrregular
	}
	return info.Mode().Type()
}

func (pe *pinDirEntry) IsDir() bool { return pe.Type().IsDir() }

type pinDummyEnt struct{}
