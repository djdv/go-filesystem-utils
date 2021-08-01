package pinfs

import (
	"github.com/djdv/go-filesystem-utils/filesystem"
	tcom "github.com/djdv/go-filesystem-utils/filesystem/interface"
)

func (pi *pinInterface) OpenDirectory(path string) (filesystem.Directory, error) {
	if path == "/" {
		return tcom.UpgradePartialStream(
			tcom.NewPartialStream(pi.ctx, &pinDirectoryStream{pinAPI: pi.core.Pin()}))
	}

	return pi.ipfs.OpenDirectory(path)
}
