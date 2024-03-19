package nfs

import (
	"io"
	"io/fs"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

var (
	_ fs.FS                        = (*FS)(nil)
	_ fs.StatFS                    = (*FS)(nil)
	_ filesystem.IDFS              = (*FS)(nil)
	_ filesystem.CreateFileFS      = (*FS)(nil)
	_ filesystem.WritableSymlinkFS = (*FS)(nil)
	_ fs.File                      = (*goFile)(nil)
	_ io.Seeker                    = (*goFile)(nil)
	_ io.Writer                    = (*goFile)(nil)
	_ fs.ReadDirFile               = (*goDirectory)(nil)
	_ fs.DirEntry                  = (*goEnt)(nil)
)
