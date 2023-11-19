package nfs

import (
	"io"
	"io/fs"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

var (
	_ fs.FS                   = (*goFS)(nil)
	_ fs.StatFS               = (*goFS)(nil)
	_ filesystem.IDFS         = (*goFS)(nil)
	_ filesystem.CreateFileFS = (*goFS)(nil)
	_ interface {
		filesystem.LinkStater
		filesystem.LinkReader
		filesystem.LinkMaker
	} = (*goFS)(nil)
	_ fs.File        = (*goFile)(nil)
	_ io.Seeker      = (*goFile)(nil)
	_ io.Writer      = (*goFile)(nil)
	_ fs.ReadDirFile = (*goDirectory)(nil)
	_ fs.DirEntry    = (*goEnt)(nil)
)
