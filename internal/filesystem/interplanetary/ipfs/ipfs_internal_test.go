package ipfs

import (
	"io/fs"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
)

var (
	_ fs.FS                    = (*FS)(nil)
	_ fs.StatFS                = (*FS)(nil)
	_ filesystem.IDFS          = (*FS)(nil)
	_ filesystem.SymlinkFS     = (*FS)(nil)
	_ fs.File                  = (*ipfsDirectory)(nil)
	_ fs.ReadDirFile           = (*ipfsDirectory)(nil)
	_ filesystem.StreamDirFile = (*ipfsDirectory)(nil)
	_ mountpoint.Marshaler     = (*FSMaker)(nil)
	_ mountpoint.FSMaker       = (*FSMaker)(nil)
	_ mountpoint.FieldParser   = (*FSMaker)(nil)
)
