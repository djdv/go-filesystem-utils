package interplanetary

import (
	"context"
	"io"
	"io/fs"
	"slices"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	coreiface "github.com/ipfs/boxo/coreiface"
)

type (
	EmptyRoot  struct{ Info *NodeInfo }
	errorEntry struct {
		fs.DirEntry // Should always be nil.
		error
	}
	CoreDirEntry struct {
		coreiface.DirEntry
		ModTime_    time.Time
		Permissions fs.FileMode
	}
)

func (er EmptyRoot) Stat() (fs.FileInfo, error) { return er.Info, nil }
func (EmptyRoot) Close() error                  { return nil }
func (EmptyRoot) Read([]byte) (int, error) {
	const op = "read"
	return -1, fserrors.New(op, filesystem.Root, filesystem.ErrIsDir, fserrors.IsDir)
}

func (EmptyRoot) ReadDir(count int) ([]fs.DirEntry, error) {
	if count > 0 {
		return nil, io.EOF
	}
	return nil, nil
}

func (ee errorEntry) Error() error { return ee.error }

func (cde *CoreDirEntry) Name() string               { return cde.DirEntry.Name }
func (cde *CoreDirEntry) IsDir() bool                { return cde.Type().IsDir() }
func (cde *CoreDirEntry) Info() (fs.FileInfo, error) { return cde, nil }
func (cde *CoreDirEntry) Size() int64                { return int64(cde.DirEntry.Size) }
func (cde *CoreDirEntry) ModTime() time.Time         { return cde.ModTime_ }
func (cde *CoreDirEntry) Mode() fs.FileMode          { return cde.Type() | cde.Permissions }
func (cde *CoreDirEntry) Sys() any                   { return cde }
func (cde *CoreDirEntry) Error() error               { return cde.DirEntry.Err }
func (cde *CoreDirEntry) Type() fs.FileMode {
	switch cde.DirEntry.Type {
	case coreiface.TDirectory:
		return fs.ModeDir
	case coreiface.TFile:
		return fs.FileMode(0)
	case coreiface.TSymlink:
		return fs.ModeSymlink
	default:
		return fs.ModeIrregular
	}
}

func DrainThenSendErr(ch chan filesystem.StreamDirEntry, err error) {
	generic.DrainBuffer(ch)
	ch <- NewErrorEntry(err)
}

func NewErrorEntry(err error) filesystem.StreamDirEntry {
	return errorEntry{error: err}
}

// TODO: Del
func NewStreamChan[T any](ch <-chan T) chan filesystem.StreamDirEntry {
	// +1 relay must account for ctx error.
	return make(chan filesystem.StreamDirEntry, cap(ch)+1)
}

func GenerateEntryChan(ctx context.Context, values []filesystem.StreamDirEntry) <-chan filesystem.StreamDirEntry {
	out := make(chan filesystem.StreamDirEntry, 1)
	go func() {
		defer close(out)
		for _, value := range values {
			if err := ctx.Err(); err != nil {
				DrainThenSendErr(out, err)
				return
			}
			select {
			case out <- value:
			case <-ctx.Done():
				DrainThenSendErr(out, ctx.Err())
				return
			}
		}
	}()
	return out
}

// accumulateRelayClose accumulates and relays
// from `entries`.
// `ctx` applies only to sends on `relay`.
// Regardless of cancellation, values will be
// received from `entries` and accumulated until it's closed.
// `relay` must have a cap of at least 1
// and will (eventually) be closed by this call.
func accumulateRelayClose(ctx context.Context,
	entries <-chan filesystem.StreamDirEntry,
	relay chan filesystem.StreamDirEntry,
	accumulator []filesystem.StreamDirEntry,
) (sawError bool, _ []filesystem.StreamDirEntry) {
	var (
		sent     int
		unsent   []filesystem.StreamDirEntry
		canceled func() bool
		relayFn  func()
	)
	canceled = func() bool {
		if err := ctx.Err(); err != nil {
			DrainThenSendErr(relay, err)
			close(relay)
			canceled = func() bool { return true }
			return true
		}
		return false
	}
	relayFn = func() {
		if canceled() {
			relayFn, unsent = func() {}, nil
			return
		}
		unsent = accumulator[sent:]
		select {
		case relay <- unsent[0]:
			sent++
			unsent = unsent[1:]
		default: // Don't wait on relay; keep caching.
		}
	}
	for entry := range entries {
		if entry.Error() != nil {
			sawError = true
		}
		accumulator = append(accumulator, entry)
		relayFn()
	}
	if canceled() {
		return sawError, accumulator
	}
	if len(unsent) == 0 {
		close(relay)
		return sawError, accumulator
	}
	// NOTE: `unsent` is slice of `accumulator`.
	clone := generic.CloneSlice(unsent) // (which could be modified by the caller.)
	go func() {
		defer close(relay)
		for _, entry := range clone {
			select {
			case relay <- entry:
			case <-ctx.Done():
				DrainThenSendErr(relay, ctx.Err())
				return
			}
		}
	}()
	return sawError, accumulator
}

// ReadEntries handles different behaviour expected by
// [fs.ReadDirFile].
// Specifically in regard to the returned values.
func ReadEntries(ctx context.Context,
	entries <-chan filesystem.StreamDirEntry, count int,
) (requested []fs.DirEntry, err error) {
	readAll := count <= 0
	if readAll {
		requested = make([]fs.DirEntry, 0, cap(entries))
	} else {
		const upperBound = 16
		requested = make([]fs.DirEntry, 0, generic.Min(count, upperBound))
	}
	requested, err = readEntriesCount(ctx, entries, requested, count)
	if err != nil && !readAll {
		requested = nil
	}
	return
}

// readEntriesCount does the actual readdir operation.
// It always returns `requested`.
func readEntriesCount(ctx context.Context,
	entries <-chan filesystem.StreamDirEntry,
	requested []fs.DirEntry,
	count int,
) ([]fs.DirEntry, error) {
	for {
		select {
		case entry, ok := <-entries:
			if !ok {
				if len(requested) == 0 {
					return requested, io.EOF
				}
				return requested, nil
			}
			if err := entry.Error(); err != nil {
				return requested, err
			}
			requested = append(requested, entry)
			if count--; count == 0 {
				return requested, nil
			}
		case <-ctx.Done():
			return requested, ctx.Err()
		}
	}
}

func ReaddirErr(op, path string, err error) error {
	if err == io.EOF {
		return err
	}
	return fserrors.New(op, path, err, fserrors.IO)
}

// AccumulateAndRelay gets a channel from `fetchEntries` and accumulates
// the values while relaying them to the returned channel.
// When the fetch channel is closed, `callbackFn` will be called
// with either the accumulated entries, or nil if an error was encountered.
func AccumulateAndRelay[
	fetchFunc func(context.Context) (<-chan filesystem.StreamDirEntry, error),
	callbackFunc func(accumulator []filesystem.StreamDirEntry),
](fetchCtx, relayCtx context.Context,
	fetchEntries fetchFunc,
	accumulator []filesystem.StreamDirEntry,
	callbackFn callbackFunc,
) (<-chan filesystem.StreamDirEntry, error) {
	var (
		fCtx, cancel = context.WithCancel(fetchCtx)
		entries, err = fetchEntries(fCtx)
	)
	if err != nil {
		cancel()
		return nil, err
	}
	const errorBuffer = 1
	relay := make(
		chan filesystem.StreamDirEntry,
		cap(entries)+errorBuffer,
	)
	go func() {
		defer cancel()
		sawError, snapshot := accumulateRelayClose(
			relayCtx, entries, relay, accumulator,
		)
		if sawError || fCtx.Err() != nil {
			snapshot = nil
		}
		callbackFn(slices.Clip(snapshot))
	}()
	return relay, nil
}
