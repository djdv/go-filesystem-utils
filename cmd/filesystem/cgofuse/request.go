package cgofuse

import (
	gopath "path"
	"strings"

	"github.com/ipfs/go-ipfs/filesystem"
)

type Request struct {
	HostPath string
	FuseArgs []string
}

func (r Request) Arguments() []string { return r.FuseArgs }
func (r Request) String() string {
	return gopath.Join("/",
		filesystem.PathProtocol.String(),
		r.hostTarget(),
	)
}

func (r Request) hostTarget() (hostPath string) {
	if r.HostPath == "" { // TODO: this is WinFSP specific and needs to be more explicit about that
		hostPath = extractUNCArg(r.FuseArgs)
	} else {
		hostPath = r.HostPath
	}
	return
}

func extractUNCArg(args []string) string {
	const uncArgPrefix = `--VolumePrefix=`
	for _, arg := range args {
		if strings.HasPrefix(arg, uncArgPrefix) {
			return `\` + strings.TrimPrefix(arg, uncArgPrefix)
		}
	}
	panic("empty host path and no path in args")
}
