//go:build !nofuse && !windows

package cgofuse

import (
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

const (
	cgoDepPanic   = "cgofuse: cannot find FUSE"
	cgoDepMessage = "A FUSE compatible library is required" +
		"to mount on this platform, but it was not found" +
		"\nSee: https://github.com/winfsp/cgofuse#readme"
)

// NOTE: [2020.04.18] cgofuse currently backed by
// hanwen/go-fuse on POSIX (excluding macOS).
// their option set doesn't support our desired options
// libfuse: opts = fmt.Sprintf(`-o fsname=ipfs,subtype=fuse.%s`, sysID.String())
// TODO: [2023.03.09] macOS may use either macFUSE or now FUSE-T
// the manuals/`--help` text for these should be consulted
// and option handling should be implemented if necessary for
// users' use cases.
// Linux needs to be checked again to see if `go-fuse`
// has added support for more libfuse options
// (directly or via some passthrough).

func makeFuseArgs(fsid filesystem.ID, host *Host) (string, []string) {
	var (
		options        strings.Builder
		uString, uSize = idOptionPre(host.UID)
		gString, gSize = idOptionPre(host.GID)
		size           = uSize + delimiterSize + gSize
	)
	options.Grow(size)
	idOption(&options, uString, 'u')
	options.WriteRune(optionDelimiter)
	idOption(&options, gString, 'g')
	var (
		fuseArgs = []string{"-o", options.String()}
		target   = host.Point
	)
	return target, fuseArgs
}
