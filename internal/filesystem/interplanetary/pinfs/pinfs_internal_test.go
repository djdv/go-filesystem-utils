package pinfs

import (
	"io/fs"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

var (
	_ fs.FS                        = (*FS)(nil)
	_ fs.StatFS                    = (*FS)(nil)
	_ filesystem.IDFS              = (*FS)(nil)
	_ filesystem.WritableSymlinkFS = (*FS)(nil)
	_ fs.File                      = (*pinDirectory)(nil)
	_ fs.ReadDirFile               = (*pinDirectory)(nil)
	_ filesystem.StreamDirFile     = (*pinDirectory)(nil)
)
