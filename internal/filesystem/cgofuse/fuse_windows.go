//go:build !nofuse

package cgofuse

import (
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

const (
	cgoDepPanic   = "cgofuse: cannot find winfsp"
	cgoDepMessage = "WinFSP(http://www.secfs.net/winfsp/) is required" +
		"to mount on this platform, but it was not found"
)

func makeFuseArgs(fsid filesystem.ID, target string) (string, []string) {
	var opts strings.Builder
	const (
		uncPrefix      = `\\`
		idOpts         = "uid=-1,gid=-1"
		systemNameOpt  = ",FileSystemName="
		volNameOpt     = ",volname="
		averageNameLen = 6
		arbitraryHint  = len(idOpts) +
			len(systemNameOpt) + len(volNameOpt) +
			averageNameLen*2
	)
	opts.Grow(arbitraryHint)
	opts.WriteString(idOpts)
	if systemName := fsid.String(); systemName != "" {
		opts.WriteString(systemNameOpt)
		opts.WriteString(systemName)
		opts.WriteString(volNameOpt)
		opts.WriteString(systemName)
	}
	fuseArgs := []string{"-o", opts.String()}
	if isUNC := strings.HasPrefix(target, uncPrefix); isUNC {
		// The UNC argument for cgo-fuse/WinFSP uses a single slash prefix.
		// And a target should not be supplied in addition to the UNC argument.
		// (This is allowed, but we want 1 or the other, not both.)
		const volumeOpt = "--VolumePrefix="
		var volOpt strings.Builder
		volOpt.Grow(len(volumeOpt) + len(target) - 1)
		volOpt.WriteString(volumeOpt)
		volOpt.WriteString(target[1:])
		return "", append(fuseArgs, volOpt.String())
	}
	return target, fuseArgs
}
