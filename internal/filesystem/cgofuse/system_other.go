//go:build !windows

package cgofuse

import (
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/winfsp/cgofuse/fuse"
)

const (
	readdirPlusCapable = false
	cgoDepPanic        = "cgofuse: cannot find FUSE"
	cgoDepMessage      = "A FUSE compatible library is required" +
		"to mount on this platform, but it was not found" +
		"\nSee: https://github.com/winfsp/cgofuse#readme"
)

func (settings *settings) hostAdjust(host *fuse.FileSystemHost) error {
	if settings.deleteAccess != nil {
		// See: cgofuse documentation
		// [fuse.SetCapDeleteAccess]
		// "Windows only"
		return generic.ConstError(
			"DenyDelete is not supported on this platform",
		)
	}
	return nil
}

func (settings *settings) makeFuseArgs(point string, fsid filesystem.ID) (string, []string) {
	var (
		options        strings.Builder
		uString, uSize = idOptionPre(settings.uid)
		gString, gSize = idOptionPre(settings.gid)
		size           = uSize + delimiterSize + gSize
	)
	options.Grow(size)
	idOption(&options, uString, 'u')
	options.WriteRune(optionDelimiter)
	idOption(&options, gString, 'g')
	fuseArgs := []string{"-o", options.String()}
	return point, fuseArgs
}

func getOSTarget(target string, _ []string) string { return target }
