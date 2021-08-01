package ipfscore

import (
	"github.com/ipfs/go-ipfs/filesystem"
	interfaceutils "github.com/ipfs/go-ipfs/filesystem/interface"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

var (
	rootStat   = &filesystem.Stat{Type: coreiface.TDirectory}
	rootFilled = filesystem.StatRequest{Type: true}
)

func (ci *coreInterface) Info(path string, req filesystem.StatRequest) (*filesystem.Stat, filesystem.StatRequest, error) {
	if path == "/" {
		return rootStat, rootFilled, nil
	}

	callCtx, cancel := interfaceutils.CallContext(ci.ctx)
	defer cancel()
	return ci.core.Stat(callCtx, ci.joinRoot(path), req)
}

func (ci *coreInterface) ExtractLink(path string) (string, error) {
	return ci.core.ExtractLink(ci.joinRoot(path))
}
