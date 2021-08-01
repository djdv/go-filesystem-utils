package cgofuse

import (
	"fmt"
	"strings"

	"github.com/ipfs/go-ipfs/filesystem"
)

func ParseRequest(sysID filesystem.ID, target string) (request Request, err error) {
	// expected target is WinFSP; use its options
	// cgofuse expects an argument format comprised of components
	// e.g. `mount.exe -o "uid=-1,volname=a valid name,gid=-1" --VolumePrefix=\localhost\UNC`
	// is equivalent to this in Go:
	//`[]string{"-o", "uid=-1,volname=a valid name,gid=-1", "--VolumePrefix=\\localhost\\UNC"}`
	// refer to the WinFSP documentation for expected parameters and their literal format

	// basic Info +
	// set the owner to be the same as the calling process
	opts := fmt.Sprintf(
		"FileSystemName=%s,volname=%s,uid=-1,gid=-1",
		sysID.String(),
		sysID.String(),
	)

	request.FuseArgs = append(request.FuseArgs, "-o", opts)

	// if target is a UNC path, assign params and return
	if len(target) > 2 && (target)[:2] == `\\` {
		// convert to WinFSP format
		// and omit the target from the request (WinFSP expects this)

		// NOTE: cgo-fuse/WinFSP UNC parameter uses single slash prefix, so we chop one off
		// the FUSE target uses `/`,
		// while the prefix parameter uses `\`
		// but otherwise they point to the same target
		request.FuseArgs = append(request.FuseArgs, fmt.Sprintf(`--VolumePrefix=%s`, target[1:]))
		return
	}

	// remove multiaddr's leading `/`
	// FIXME: ^ formatter will show `I:` as `/I:` as well
	target = strings.TrimPrefix(target, "/")
	request.HostPath = target
	return
}
