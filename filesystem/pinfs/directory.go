package pinfs

import (
	"context"
	"io/fs"
	"path"

	"github.com/djdv/go-filesystem-utils/filesystem/errors"
	gofs "github.com/djdv/go-filesystem-utils/filesystem/go"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	coreoptions "github.com/ipfs/interface-go-ipfs-core/options"
)

// TODO: we should probablt split sequential directories from stream variants
// one can embed the other - stream should be lighter and preferred (like the original was)
type pinDirectory struct {
	ctx    context.Context
	cancel context.CancelFunc
	stat   *rootStat
	ipfs   fs.FS

	// Used in Steam
	pinAPI coreiface.PinAPI
	pins   <-chan coreiface.Pin

	// Used in Read
	transformed <-chan fs.DirEntry
	errs        <-chan error
}

func (pd *pinDirectory) Stat() (fs.FileInfo, error) { return pd.stat, nil }

func (*pinDirectory) Read([]byte) (int, error) {
	const op errors.Op = "pinDirectory.Read"
	return -1, errors.New(op, errors.IsDir)
}

func (pd *pinDirectory) StreamDir(ctx context.Context, output chan<- fs.DirEntry) <-chan error {
	const op errors.Op = "pinDirectory.StreamDir"
	pins := pd.pins
	if pins == nil {
		pinChan, err := pd.pinAPI.Ls(pd.ctx, coreoptions.Pin.Ls.Recursive())
		if err != nil {
			errs := make(chan error, 1)
			errs <- errors.New(op,
				errors.IO,
				err,
			)
			close(errs)
			return errs
		}
		pins = pinChan
		pd.pins = pins
	}

	errs := make(chan error)
	go func() {
		defer close(output)
		for pins != nil {
			select {
			case pin, ok := <-pins:
				if !ok {
					pins = nil
					pd.pins = nil // FIXME: thread safety
					break
				}
				if err := pin.Err(); err != nil {
					select {
					case errs <- err:
						continue
					case <-ctx.Done():
						return
					}
				}
				select {
				case output <- &pinDirEntry{Pin: pin, ipfs: pd.ipfs}:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return errs
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func (pd *pinDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	var (
		entries = pd.transformed
		errs    = pd.errs
	)
	if entries == nil {
		output := make(chan fs.DirEntry, max(0, count))
		errs = pd.StreamDir(pd.ctx, output)
		pd.transformed = output
		pd.errs = errs
		entries = output
	}
	return gofs.ReadDir(count, entries)
}

type pinDirEntry struct {
	coreiface.Pin
	ipfs fs.FS
}

func (pe *pinDirEntry) Name() string { return path.Base(pe.Pin.Path().String()) }

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
