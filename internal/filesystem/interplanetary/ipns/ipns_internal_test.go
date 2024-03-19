package ipns

import (
	"io"
	"io/fs"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
)

var (
	_ fs.FS                  = (*FS)(nil)
	_ fs.StatFS              = (*FS)(nil)
	_ filesystem.IDFS        = (*FS)(nil)
	_ filesystem.SymlinkFS   = (*FS)(nil)
	_ fs.File                = (*ipnsFile)(nil)
	_ fs.ReadDirFile         = (*ipnsFile)(nil)
	_ io.Seeker              = (*ipnsFile)(nil)
	_ mountpoint.Marshaler   = (*FSMaker)(nil)
	_ mountpoint.FSMaker     = (*FSMaker)(nil)
	_ mountpoint.FieldParser = (*FSMaker)(nil)
)
