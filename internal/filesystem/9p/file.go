package p9

import (
	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
)

type (
	ioUnit   = uint32
	noopFile = templatefs.NoopFile
	link     = linkSettings
	openFlag bool
	file     struct {
		noopFile
		metadata
		link
		openFlag
	}
)

const noIOUnit ioUnit = 0

func (b openFlag) fidOpened() bool { return bool(b) }

func (fi *file) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	return fi.metadata.SetAttr(valid, attr)
}

func (fi *file) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return fi.metadata.GetAttr(req)
}
