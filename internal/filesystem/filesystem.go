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
	// TODO: [review] These interfaces come from our pre fs.FS implementations
	// Things like names and patterns may (need to) change.
	// Some things might need to be added as well.
	IDFS interface {
		fs.FS
		ID() ID
	}
	MakeFileFS interface {
		fs.FS
		MakeFile(string) error
	}
	RemoveFS interface {
		fs.FS
		Remove(string) error
	}
	SymlinkFS interface {
		fs.FS
		// TODO: better name?
		// Probably should be explicit to avoid namespace collisions
		// with hard-linkers.
		MakeLink(string, string) error
		// TODO: This is legacy and should be changed
		// We used to collide with "ReadLink".
		// We can use that name, or something else now.
		ExtractLink(string) (string, error)
	}
	RenameFS interface {
		fs.FS
		// TODO: We need to document expectations for this.
		// Specifically what error value is expected to be returned
		// if things like type constraints are enforced for the system.
		// I.e. "... should return [ErrX] if old is type X
		// and new is type Y and the system disallows this"
		// ^ But in English [Ame].
		Rename(oldName, newName string) error
	}
	/* TODO
	TruncateFileFS interface {
	fs.FS
		Truncate(path string, size uint64) error
	}
	*/
	MakeDirectoryFS interface {
		fs.FS
		MakeDirectory(string) error
	}
	RemoveDirectoryFS interface {
		fs.FS
		RemoveDirectory(string) error
	}

	TruncateFile interface {
		fs.File
		// TODO: [review] Decimal type should be considered
		// What do other systems use, and more importantly why.
		// Is a negative size ever a valid request in any file system?
		Truncate(size uint64) error
	}

	// TODO: fs.FS interface names seem to deviate from the usuer 'er' suffix.
	// These should probably use the XInfo style, as is in xFile, xFS, et al.
	AccessTimer interface {
		fs.FileInfo
		AccessTime() time.Time
	}
	ChangeTimer interface {
		fs.FileInfo
		ChangeTime() time.Time
	}
	CreationTimer interface {
		fs.FileInfo
		CreationTime() time.Time
	}

	// TODO: [review] it might be better to
	// not make composite types like this in this pkg.
	// the fuse pkg can just do this internally.
	POSIXInfo interface {
		fs.FileInfo
		AccessTimer
		ChangeTimer
		// TODO: We'll should probably add the full set from SUSv4;BSi7.
	}
	// TODO: name; would StreamDirEntry make more sense? Something else?
	// (We want to at least try to be somewhat consistent with the fs.FS conventions.)
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
