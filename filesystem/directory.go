package filesystem

import (
	"context"
	"io"
)

// TODO: pass on docs
// Directories return a channel of their entries, which contain a name and an offset.
// The initial call to List must have an offset value of 0.
// Subsequent calls to List with a non-0 offset shall replay the stream exactly, starting at the provided offset.
// (replay meaning: entries are returned in the same order with the same values)
// Calling Reset shall reset the stream as if it had just been opened.
// Previous offset values may be considered invalid after a Reset, but are not required to be.
type Directory interface {
	// List attempts to return all entries starting from `offset`
	// `offset` values must be either 0 or a value previously provided by `DirectoryEntry.Offset()`
	// the returned channel is closed under the following conditions:
	//  - The context is canceled
	//  - The end of the listing is reached
	//  - An error was encountered during listing
	// if an error is encountered during listing, an entry is returned containing it
	// (prior to closing the channel)
	List(ctx context.Context, offset uint64) <-chan DirectoryEntry
	// Reset will cause the `Directory` to reinitialize itself
	// TODO: this might be better named `Refresh`
	// we also need to better define its purpose and relation to `List`
	// it was needed to mimic SUS's `rewinddir`
	// but we kind of don't want that to be tied to the interface so much as the implementations
	Reset() error
	io.Closer
}

// DirectoryEntry contains basic information about an entry within a directory
// returned from `List`, it specifies the offset value for the next entry to be listed
// you may provide this value to `List` if you wish to resume an interrupted listing
// or replay a listing, from this entry.
// TODO: document all the nonsense around offset values and how they relate to `Reset`
// (^ just some note about implementation specific blah blah)
type DirectoryEntry interface {
	Name() string
	Offset() uint64
	Error() error
}
