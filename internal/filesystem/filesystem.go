package filesystem

import (
	"context"
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
