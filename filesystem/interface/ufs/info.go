package ufs

import (
	"github.com/ipfs/go-ipfs/filesystem"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	corepath "github.com/ipfs/interface-go-ipfs-core/path"
)

var (
	rootStat   = &filesystem.Stat{Type: coreiface.TDirectory}
	rootFilled = filesystem.StatRequest{Type: true}
)

func (ui *ufsInterface) Info(path string, req filesystem.StatRequest) (attr *filesystem.Stat, filled filesystem.StatRequest, err error) {
	if path == "/" {
		return rootStat, rootFilled, nil
	}

	return ui.core.Stat(ui.ctx, corepath.New(path), req)
}

func (ui *ufsInterface) ExtractLink(path string) (string, error) {
	return ui.core.ExtractLink(corepath.New(path))
}
