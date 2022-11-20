package filesystem

import (
	"context"
	"io"
	"io/fs"
	"time"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	coreoptions "github.com/ipfs/interface-go-ipfs-core/options"
)

// TODO: move this to a test file
var _ StreamDirFile = (*pinStream)(nil)

type (
	IPFSPinAPI struct {
		pinAPI coreiface.PinAPI
		ipfs   fs.FS // TODO: subsys should be handled via `bind` instead? fs.Subsys?
	}
	pinStream struct {
		stat fs.FileInfo
		pins <-chan coreiface.Pin
		context.Context
		context.CancelFunc
		ipfs fs.FS
	}
	pinDirEntry struct {
		coreiface.Pin
		ipfs fs.FS // TODO: replace this with statfunc or something. We shouldn't need the whole FS.
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
		return pfs.openRoot()
	}
	if subsys := pfs.ipfs; subsys != nil {
		return subsys.Open(name)
	}
	return nil, &fs.PathError{
		Op:   op,
		Path: name,
		Err:  fserrors.New(fserrors.NotExist), // TODO old-style err
	}
}

func (pfs *IPFSPinAPI) openRoot() (fs.ReadDirFile, error) {
	const op fserrors.Op = "pinfs.openRoot"
	var (
		ctx, cancel = context.WithCancel(context.Background())
		lsOpts      = []coreoptions.PinLsOption{
			coreoptions.Pin.Ls.Recursive(),
		}
		pins, err = pfs.pinAPI.Ls(ctx, lsOpts...)
	)
	if err != nil {
		cancel()
		return nil, &fs.PathError{ // TODO old-style err; convert to wrapped, defined, const errs.
			Op:   "open", // TODO: what does the fs.FS spec say for extensions? `opendir`?
			Path: rootName,
			Err: fserrors.New(op,
				fserrors.IO,
				err),
		}
	}
	// TODO: retrieve permission from somewhere else. (Passed into FS constructor)
	const permissions = s_IRXA
	stream := &pinStream{
		ipfs:       pfs.ipfs,
		pins:       pins,
		Context:    ctx,
		CancelFunc: cancel,
		stat: staticStat{
			name:    rootName,
			mode:    fs.ModeDir | permissions,
			modTime: time.Now(), // Not really modified, but pin-set as-of right now.
		},
	}
	return stream, nil
}

func (ps *pinStream) Stat() (fs.FileInfo, error) { return ps.stat, nil }

func (*pinStream) Read([]byte) (int, error) {
	const op fserrors.Op = "pinStream.Read"
	return -1, fserrors.New(op, fserrors.IsDir)
}

// TODO: also implement StreamDirFile
func (ps *pinStream) ReadDir(count int) ([]fs.DirEntry, error) {
	const op fserrors.Op = "pinStream.ReadDir"
	var (
		ctx  = ps.Context
		pins = ps.pins
	)
	if ctx == nil ||
		pins == nil {
		return nil, fserrors.New(op, fserrors.IO) // TODO: error value for E-not-open?
	}

	const upperBound = 64
	var (
		ipfs      = ps.ipfs
		entries   = make([]fs.DirEntry, 0, generic.Min(count, upperBound))
		returnAll = count <= 0
	)
	for {
		select {
		case <-ctx.Done():
			return entries, ctx.Err()
		case pin, ok := <-pins:
			if !ok {
				var err error
				if !returnAll {
					err = io.EOF
				}
				return entries, err
			}
			if err := pin.Err(); err != nil {
				return entries, err
			}
			entries = append(entries, translatePinEntry(pin, ipfs))
			if !returnAll {
				if count--; count == 0 {
					return entries, nil
				}
			}
		}
	}
}

func translatePinEntry(pin coreiface.Pin, ipfs fs.FS) fs.DirEntry {
	return &pinDirEntry{
		Pin:  pin,
		ipfs: ipfs,
	}
}

func (ps *pinStream) StreamDir(ctx context.Context) <-chan DirStreamEntry {
	var (
		pins    = ps.pins
		ipfs    = ps.ipfs
		entries = make(chan DirStreamEntry, cap(pins))
	)
	go func() {
		defer close(entries)
		if pins != nil {
			translatePinEntries(ctx, pins, entries, ipfs)
			return
		}
		const op fserrors.Op = "pinStream.StreamDir"
		err := fserrors.New(op, fserrors.IO) // TODO: error value for E-not-open?
		select {
		case entries <- newErrorEntry(err):
		case <-ctx.Done():
		}
	}()
	return entries
}

func translatePinEntries(ctx context.Context,
	pins <-chan coreiface.Pin,
	entries chan<- DirStreamEntry,
	ipfs fs.FS,
) {
	for pin := range pins {
		var entry DirStreamEntry
		if err := pin.Err(); err != nil {
			entry = newErrorEntry(err)
		} else {
			entry = wrapDirEntry(translatePinEntry(pin, ipfs))
		}
		select {
		case entries <- entry:
		case <-ctx.Done():
			return
		}
	}
}

func (ps *pinStream) Close() error {
	const op fserrors.Op = "pinStream.Close"
	if cancel := ps.CancelFunc; cancel != nil {
		cancel()
		ps.Context = nil
		ps.CancelFunc = nil
		ps.pins = nil
		return nil
	}
	return fserrors.New(op,
		fserrors.InvalidItem, // TODO: Check POSIX expected values
		"directory stream was not open",
	)
}

func (pe *pinDirEntry) Name() string {
	return pe.Pin.Path().Cid().String()
}

func (pe *pinDirEntry) Info() (fs.FileInfo, error) {
	pinCid := pe.Pin.Path().Cid()
	if ipfs := pe.ipfs; ipfs != nil {
		return fs.Stat(pe.ipfs, pinCid.String())
	}
	return staticStat{
		name:    pinCid.String(),
		mode:    fs.ModeDir | s_IRWXA, // TODO: permission come from somewhere else.
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
