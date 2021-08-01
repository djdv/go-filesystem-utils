package interfaceutils

import (
	"context"
	"fmt"
	"sync"

	"github.com/ipfs/go-ipfs/filesystem"
)

// fullEntry simply extends a `PartialEntry`, giving it an offset value.
type fullEntry struct {
	PartialEntry
	offset uint64
}

func (fe *fullEntry) Offset() uint64 { return fe.offset }

// errEntry is a `filesystem.DirectoryEntry` containing only an error.
type errorEntry struct{ Err error }

func (ee *errorEntry) Name() string   { return "" }
func (ee *errorEntry) Offset() uint64 { return 0 }
func (ee *errorEntry) Error() error   { return ee.Err }

// errWrap takes an error and returns a single entry stream containing it.
func errWrap(err error) <-chan filesystem.DirectoryEntry {
	errChan := make(chan filesystem.DirectoryEntry, 1)
	errChan <- &errorEntry{err}
	close(errChan)
	return errChan
}

// EntryStorage implements `filesystem.Directory.List` by utilizing a `PartialStream`
// storing the entries it lists out, and assigning them offset values that persist across calls.
type EntryStorage interface {
	List(context.Context, uint64) <-chan filesystem.DirectoryEntry
	Reset(<-chan PartialEntry)
}

func NewEntryStorage(streamSource <-chan PartialEntry) EntryStorage {
	return &entryStorage{
		entryStore:   make([]filesystem.DirectoryEntry, 0),
		sourceStream: streamSource,
	}
}

type entryStorage struct {
	tail         uint64
	entryStore   []filesystem.DirectoryEntry
	sourceStream <-chan PartialEntry
	sync.Mutex
}

func (es *entryStorage) head() uint64 { return es.tail - uint64(len(es.entryStore)) }

func (es *entryStorage) List(ctx context.Context, offset uint64) <-chan filesystem.DirectoryEntry {
	es.Lock()

	// NOTE:
	// Offset values in our system are unique per stream instance,
	// and previously valid values become invalid when the stream is `Reset()`
	// as such, we validate these offset bounds below.
	//
	// offset 0 is a special exception to our lower bound
	// it reads/replays from the beginning of the stream
	cursor := offset
	if offset != 0 {
		// provided offset must be within range:
		// lower bound - our streams head, which is also the leftmost stored entry's "absolute offset"
		// upper bound - our streams tail, as incremented by each read from the underlying source stream
		if offset < es.head() || offset > es.tail {
			es.Unlock()
			return errWrap(fmt.Errorf("offset %d is not/no-longer valid", offset))
		}

		// we checked above that the offset value is within our accepted range
		// now we need to do the actual conversion from the absolute value that we previously provided
		// back to a relative index
		// which will be either an index of our cached entries
		// or 1 beyond its end, signifying we should read from the stream instead

		// reduce the provided "absolute offset" to an index within our imaginary boundary [head,tail]
		// then further reduce it relative to the store's boundaries [0,len(store)]
		cursor = (offset % (es.tail + 1)) % uint64(len(es.entryStore)+1)
	}

	listChan := make(chan filesystem.DirectoryEntry)

	go func() {
		defer close(listChan)
		defer es.Unlock()
		// if cursor is within store range, pull entries from it first
		if cursor < uint64(len(es.entryStore)) {
			for _, ent := range es.entryStore[cursor:] {
				select {
				case <-ctx.Done(): // list was canceled; we're done
					return

				case listChan <- ent: // relay the entry to the caller
					cursor++ // and move forward
				}
			}
		}

		// pull entries from the stream
		for {
			select {
			case <-ctx.Done(): // `List` was canceled before we read the source stream
				return

			default:
				ent, ok := <-es.sourceStream
				if !ok { // end of stream
					return
				}

				es.tail++                                      // underlying stream was read, so advance the tail
				fullEnt := &fullEntry{ent, es.tail}            // use it as the offset value for the entry
				es.entryStore = append(es.entryStore, fullEnt) // and add it to the store

				// between getting the entry from the stream and now
				// we may have been or may become canceled, before listChan is actually read by the caller
				select {
				case <-ctx.Done():
					return
				case listChan <- fullEnt:
				}
			}
		}
	}()
	return listChan
}

func (es *entryStorage) Reset(streamSource <-chan PartialEntry) {
	es.Lock()
	defer es.Unlock()

	for i := range es.entryStore { // clear the store (so the gc can reap the entries)
		es.entryStore[i] = nil
	}
	es.entryStore = es.entryStore[:0] // reslice it (use the same slice to avoid realloc)

	es.sourceStream = streamSource // replace the underlying stream

	// Invalidate the rightmost offset value
	// Whether it pointed to a valid entry (on the next read)
	// or was pointing at the end of the stream; we consider it an invalid value/request past this point
	// The (new) leftmost offset will be based on this (new) boundary value
	// (i.e. the next entry read will be resident/relative 0 of/to the entryStore)
	// (e.g. if the rightmost entry has offset value 1, and was stored in slot 0
	// a reset would cause the next entry read for slot 0, to hold an offset value of 3
	// invalidating 2 altogether)
	if es.tail != 0 { // (we don't do this if the directory hasn't actually been read)
		es.tail++
	}
}
