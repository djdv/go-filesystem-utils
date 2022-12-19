//go:build nofuse

package cgofuse

import (
	"errors"
	"io/fs"
)

type Fuse struct{}

var errNoFuse = errors.New("fuse support not included in this build")

func FSToFuse(fs.FS, ...WrapperOption) (*Fuse, error) { return nil, errNoFuse }
func (fh *Fuse) Mount(string) error                   { return errNoFuse }
func (fh *Fuse) Unmount(string) error                 { return errNoFuse }
func (fh *Fuse) Close() error                         { return errNoFuse }
