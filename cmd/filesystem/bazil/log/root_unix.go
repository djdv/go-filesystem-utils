//go:build !nofuse && !(windows || plan9 || netbsd || openbsd)
// +build !nofuse,!windows,!plan9,!netbsd,!openbsd

package log

import (
	"context"

	fuse "bazil.org/fuse"
	fs "bazil.org/fuse/fs"
	logging "github.com/ipfs/go-log"
)

type (
	FileSystem struct{ logging.EventLogger }
	Root       struct{ logging.EventLogger }
)

func NewFileSystem(log logging.EventLogger) (FileSystem, error) {
	return FileSystem{EventLogger: log}, nil
}

func (f *FileSystem) Root() (_ fs.Node, _ error)                 { f.Debug("filesystem, get root"); return }
func (f *FileSystem) Destroy()                                   { f.Debug("destroy called"); f = nil }
func (r *Root) Attr(ctx context.Context, a *fuse.Attr) (_ error) { r.Debug("Root Attr"); return }
func (r *Root) Forget()                                          { r.Debug("Root Forget") }

func (r *Root) Lookup(_ context.Context, name string) (_ fs.Node, _ error) {
	r.Debugf("Root Lookup: '%s'", name)
	return
}

func (r *Root) ReadDirAll(_ context.Context) (_ []fuse.Dirent, _ error) {
	r.Debug("Root ReadDirAll")
	return
}
