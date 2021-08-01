package cgofuse

import (
	"github.com/ipfs/go-ipfs/filesystem"
)

func ParseRequest(sysID filesystem.ID, target string) (request Request, err error) {
	// [2020.04.18] cgofuse currently backed by hanwen/go-fuse on linux
	// their option set doesn't support our desired options
	// libfuse: opts = fmt.Sprintf(`-o fsname=ipfs,subtype=fuse.%s`, sysID.String())
	request.HostPath = target
	return
}
