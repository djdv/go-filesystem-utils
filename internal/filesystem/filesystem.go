package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"
)

type (
	AccessTimer interface {
		AccessTime() time.Time
	}
	ChangeTimer interface {
		ChangeTime() time.Time
	}
	CreationTimer interface {
		CreationTime() time.Time
	}
	POSIXInfo interface {
		fs.FileInfo
		AccessTimer
		ChangeTimer
		// TODO: We'll should probably add the full set from SUSv4;BSi7.
	}
	DirStreamEntry interface {
		fs.DirEntry
		Error() error
	}
	StreamDirFile interface {
		fs.ReadDirFile
		StreamDir(ctx context.Context) <-chan DirStreamEntry
	}
)

func StreamDir(ctx context.Context, directory fs.ReadDirFile) <-chan DirStreamEntry {
	if dirStreamer, ok := directory.(StreamDirFile); ok {
		return dirStreamer.StreamDir(ctx)
	}
	stream := make(chan DirStreamEntry)
	go func() {
		defer close(stream)
		for {
			var (
				entry DirStreamEntry
				// TODO: this is very inefficient.
				// We should probably port the old entry cache over,
				// and read batches at a time.
				ents, err = directory.ReadDir(1)
			)
			switch {
			case err != nil:
				if errors.Is(err, io.EOF) {
					return
				}
				entry = &errorDirectoryEntry{error: err}
			case len(ents) != 1:
				// TODO: real error message
				err := fmt.Errorf("unexpected count for [fs.ReadDir]"+
					"\n\tgot: %d"+
					"\n\twant: %d",
					len(ents), 1,
				)
				entry = &errorDirectoryEntry{error: err}
			default:
				entry = dirEntryWrapper{DirEntry: ents[0]}
			}
			select {
			case <-ctx.Done():
				return
			case stream <- entry:
			}
		}
	}()
	return stream
}
