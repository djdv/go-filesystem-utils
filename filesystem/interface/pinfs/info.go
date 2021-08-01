package pinfs

import (
	"github.com/ipfs/go-ipfs/filesystem"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

var (
	rootStat   = &filesystem.Stat{Type: coreiface.TDirectory}
	rootFilled = filesystem.StatRequest{Type: true}
)

func (pi *pinInterface) Info(path string, req filesystem.StatRequest) (*filesystem.Stat, filesystem.StatRequest, error) {
	if path == "/" {
		return rootStat, rootFilled, nil
	}
	return pi.ipfs.Info(path, req)
}

func (pi *pinInterface) ExtractLink(path string) (string, error) { return pi.ipfs.ExtractLink(path) }
