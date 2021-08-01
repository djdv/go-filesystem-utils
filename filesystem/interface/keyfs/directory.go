package keyfs

import (
	"github.com/djdv/go-filesystem-utils/filesystem"
	tcom "github.com/djdv/go-filesystem-utils/filesystem/interface"
)

func (ki *keyInterface) OpenDirectory(path string) (filesystem.Directory, error) {
	fs, _, fsPath, deferFunc, err := ki.selectFS(path)
	if err != nil {
		return nil, err
	}
	defer deferFunc()

	if fs == ki {
		return tcom.UpgradePartialStream(
			tcom.NewPartialStream(ki.ctx, &keyDirectoryStream{keyAPI: ki.core.Key()}))
	}

	return fs.OpenDirectory(fsPath)
}
