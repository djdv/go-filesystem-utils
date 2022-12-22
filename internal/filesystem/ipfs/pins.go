package ipfs

import (
	"context"
	"io"
	"io/fs"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	coreoptions "github.com/ipfs/interface-go-ipfs-core/options"
)

var ( // TODO: move this to a test file
	_ filesystem.IDFS          = (*IPFSPinFS)(nil)
	_ filesystem.StreamDirFile = (*pinDirectory)(nil)
	// TODO:
	// _ POSIXInfo     = (*pinDirEntry)(nil)
)

type (
	IPFSPinFS struct {
		pinAPI coreiface.PinAPI
		ipfs   fs.FS // TODO: subsys should be handled via `bind` instead? fs.Subsys?
	}
	pinDirectory struct {
		modTime time.Time
		pins    <-chan coreiface.Pin
		context.Context
		context.CancelFunc
		ipfs fs.FS
		mode fs.FileMode
	}

	pinDirEntry struct {
		coreiface.Pin
		ipfs fs.FS // TODO: replace this with statfunc or something. We shouldn't need the whole FS.
	}
	pinInfo struct { // TODO: roll into pinDirEntry?
		name     string
		mode     fs.FileMode // Without the type, this is only really useful for move+delete permissions.
		accessed time.Time
	}
)

func NewPinFS(pinAPI coreiface.PinAPI, options ...PinfsOption) *IPFSPinFS {
	fs := &IPFSPinFS{pinAPI: pinAPI}
	for _, setter := range options {
		if err := setter(fs); err != nil {
			panic(err)
		}
	}
	return fs
}

func (*IPFSPinFS) ID() filesystem.ID { return filesystem.IPFSPins }

func (pfs *IPFSPinFS) Open(name string) (fs.File, error) {
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

func (pfs *IPFSPinFS) openRoot() (fs.ReadDirFile, error) {
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
	const permissions = readAll | executeAll
	stream := &pinDirectory{
		mode:       fs.ModeDir | permissions,
		modTime:    time.Now(),
		ipfs:       pfs.ipfs,
		pins:       pins,
		Context:    ctx,
		CancelFunc: cancel,
	}
	return stream, nil
}

func (*pinDirectory) Name() string                  { return rootName }
func (*pinDirectory) Size() int64                   { return 0 }
func (ps *pinDirectory) Stat() (fs.FileInfo, error) { return ps, nil }
func (ps *pinDirectory) Mode() fs.FileMode          { return ps.mode }
func (ps *pinDirectory) ModTime() time.Time         { return ps.modTime }
func (ps *pinDirectory) IsDir() bool                { return ps.Mode().IsDir() }
func (ps *pinDirectory) Sys() any                   { return ps }

func (*pinDirectory) Read([]byte) (int, error) {
	const op fserrors.Op = "pinStream.Read"
	return -1, fserrors.New(op, fserrors.IsDir)
}

// TODO: also implement StreamDirFile
func (ps *pinDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
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
					// FIXME: update this to match standard expectations (like is done in core)
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

func translatePinEntry(pin coreiface.Pin, ipfs fs.FS) filesystem.StreamDirEntry {
	return &pinDirEntry{
		Pin:  pin,
		ipfs: ipfs,
	}
}

func (ps *pinDirectory) StreamDir(ctx context.Context) <-chan filesystem.StreamDirEntry {
	var (
		pins    = ps.pins
		ipfs    = ps.ipfs
		entries = make(chan filesystem.StreamDirEntry, cap(pins))
	)
	go func() {
		defer close(entries)
		if pins != nil {
			translatePinEntries(ctx, pins, entries, ipfs)
		}
	}()
	return entries
}

func translatePinEntries(ctx context.Context,
	pins <-chan coreiface.Pin,
	entries chan<- filesystem.StreamDirEntry,
	ipfs fs.FS,
) {
	for pin := range pins {
		select {
		case entries <- translatePinEntry(pin, ipfs):
		case <-ctx.Done():
			return
		}
	}
}

func (ps *pinDirectory) Close() error {
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
	// TODO: permission come from somewhere else.
	const permissions = readAll | executeAll
	return &pinInfo{
		name:     pinCid.String(),
		mode:     fs.ModeDir | permissions,
		accessed: time.Now(),
	}, nil
}

func (pe *pinDirEntry) Type() fs.FileMode {
	info, err := pe.Info()
	if err != nil {
		return fs.ModeIrregular
	}
	return info.Mode().Type()
}

func (pe *pinDirEntry) IsDir() bool  { return pe.Type().IsDir() }
func (pe *pinDirEntry) Error() error { return pe.Pin.Err() }

func (pi *pinInfo) Name() string       { return pi.name }
func (*pinInfo) Size() int64           { return 0 } // Unknown without IPFS subsystem.
func (pi *pinInfo) Mode() fs.FileMode  { return pi.mode }
func (pi *pinInfo) ModTime() time.Time { return pi.accessed }
func (pi *pinInfo) IsDir() bool        { return pi.Mode().IsDir() }
func (pi *pinInfo) Sys() any           { return pi }
