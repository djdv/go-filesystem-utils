package files

import (
	"sync"
	"sync/atomic"

	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
)

type (
	file     = p9.File
	noopFile = templatefs.NoopFile
	ninePath = *atomic.Uint64

	File struct {
		noopFile
		metadata
		link
		openFlag
	}

	// TODO: [7dd5513d-4991-46c9-8632-fc36475e88a8] This has shown up again.
	deferMutex struct{ sync.Mutex }
)

func (fi *File) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	return fi.metadata.SetAttr(valid, attr)
}

func (fi *File) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return fi.metadata.GetAttr(req)
}
