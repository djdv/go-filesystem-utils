package cgofuse

import (
	"github.com/ipfs/go-ipfs/filesystem"
)

func ParseRequest(sysID filesystem.ID, target string) (request Request, err error) {
	request.HostPath = target
	request.FuseArgs = append(request.FuseArgs, "-o", "allow_other")
	return
}
