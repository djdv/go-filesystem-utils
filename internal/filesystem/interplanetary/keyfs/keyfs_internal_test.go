package keyfs

import (
	"io/fs"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

var (
	_ fs.FS                        = (*FS)(nil)
	_ fs.StatFS                    = (*FS)(nil)
	_ filesystem.IDFS              = (*FS)(nil)
	_ filesystem.WritableSymlinkFS = (*FS)(nil)
	_ fs.File                      = (*keyDirectory)(nil)
	_ fs.ReadDirFile               = (*keyDirectory)(nil)
)
