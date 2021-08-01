package filesystem

import (
	"io"
)

type File interface {
	io.ReadWriteCloser
	io.Seeker
	Size() (int64, error)
	Truncate(uint64) error
}
