package interfaceutils

import (
	"context"
	"errors"
	"io"

	"github.com/ipfs/go-ipfs/filesystem"
)

var (
	ErrIsOpen         = errors.New("already open")
	ErrNotOpen        = errors.New("not opened")
	ErrNotInitialized = errors.New("directory not initialized")
)

// PartialEntry is intended to be extended into a full `filesystem.DirectoryEntry`.
type PartialEntry interface {
	Name() string
	Error() error
}

// PartialStream implements a stream of `PartialEntry`s  that can be (re)opened and closed.
type PartialStream interface {
	Open() (<-chan PartialEntry, error)
	io.Closer
}

// PartialStreamGenerator will receive `SendTo` requests.
// Instructing it to generate a stream of `PartialEntry`s and start sending them to a receiver.
type PartialStreamGenerator interface {
	// SendTo is to generate a stream
	// and start (asynchronously) sending entries to the receiver channel it was passed
	// If the stream cannot be generated, an error is to be returned
	// The provided channel must be closed under any of the following conditions:
	//  - The end of the stream is reached
	//  - The context is canceled
	//  - The stream could not be generated and an error is being returned
	SendTo(context.Context, chan<- PartialEntry) error
}

// streamBase implements a basic `PartialStream` by utilizing a `PartialStreamGenerator`
// to handle `Open` and `Close` requests.
type streamBase struct {
	parentCtx       context.Context
	streamCancel    context.CancelFunc
	streamGenerator PartialStreamGenerator
}

func NewPartialStream(ctx context.Context, sg PartialStreamGenerator) PartialStream {
	return &streamBase{
		parentCtx:       ctx,
		streamGenerator: sg,
	}
}

func (ps *streamBase) Open() (<-chan PartialEntry, error) {
	if ps.streamCancel != nil {
		return nil, ErrIsOpen
	}

	streamCtx, streamCancel := context.WithCancel(ps.parentCtx)
	listChan := make(chan PartialEntry, 1) // SendTo is responsible for this channel

	if err := ps.streamGenerator.SendTo(streamCtx, listChan); err != nil {
		streamCancel()
		return nil, err
	}
	ps.streamCancel = streamCancel

	return listChan, nil
}

func (ps *streamBase) Close() error {
	if ps.streamCancel == nil {
		return ErrNotOpen // we consider double close an error
	}

	ps.streamCancel()
	ps.streamCancel = nil

	return nil
}

// partialStreamWrapper implements a full `filesystem.Directory`.
// Utilizing a `PartialStream` and `EntryStorage` to
// support requests to `List` that contain an offset.
type partialStreamWrapper struct {
	PartialStream       // actual source of entries
	EntryStorage        // storage and offset management for them
	err           error // errors persist across calls; cleared on Reset
}

// UpgradePartialStream adds seeking/offset support to a `PartialStream`,
// upgrading it into a full `filesystem.Directory`.
func UpgradePartialStream(streamSource PartialStream) (filesystem.Directory, error) {
	stream, err := streamSource.Open()
	if err != nil {
		return nil, err
	}

	return &partialStreamWrapper{
		PartialStream: streamSource,
		EntryStorage:  NewEntryStorage(stream),
	}, nil
}

func (ps *partialStreamWrapper) Reset() error {
	if err := ps.PartialStream.Close(); err != nil { // invalidate the old stream
		ps.err = err
		return err
	}

	stream, err := ps.PartialStream.Open()
	if err != nil { // get a new stream
		ps.err = err
		return err
	}

	ps.EntryStorage.Reset(stream) // reset the entry store

	ps.err = nil // clear error state, if any
	return nil
}

func (ps *partialStreamWrapper) List(ctx context.Context, offset uint64) <-chan filesystem.DirectoryEntry {
	if ps.err != nil { // refuse to operate
		return errWrap(ps.err)
	}

	if ps.EntryStorage == nil {
		err := ErrNotInitialized
		ps.err = err
		return errWrap(err)
	}

	return ps.EntryStorage.List(ctx, offset)
}
