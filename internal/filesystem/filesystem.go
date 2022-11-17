package filesystem

import (
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

	OpenDirFS interface {
		fs.FS
		OpenDir(name string) (fs.ReadDirFile, error)
	}
)
