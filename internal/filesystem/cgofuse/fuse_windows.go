package cgofuse

import (
	"os"
	"strconv"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

const (
	cgoDepPanic   = "cgofuse: cannot find winfsp"
	cgoDepMessage = "WinFSP(http://www.secfs.net/winfsp/) is required" +
		" to mount on this platform, but it was not found"

	systemNameOpt = "FileSystemName="
	volNameOpt    = "volname="
	volumeOpt     = "--VolumePrefix="
)

func makeFuseArgs(fsid filesystem.ID, host *Host) (string, []string) {
	const uncPrefix = `\\`
	var (
		options        strings.Builder
		uString, uSize = idOptionPre(host.UID)
		gString, gSize = idOptionPre(host.GID)
		nameSize       = nameOptionSize(fsid)
		size           = uSize + delimiterSize +
			gSize + nameSize
	)
	if nameSize != 0 {
		size += delimiterSize
	}
	options.Grow(size)
	idOption(&options, uString, 'u')
	options.WriteRune(optionDelimiter)
	idOption(&options, gString, 'g')
	if nameSize != 0 {
		options.WriteRune(optionDelimiter)
		nameOption(&options, fsid)
	}
	fuseArgs := []string{"-o", options.String()}
	// The UNC argument for cgo-fuse/WinFSP uses a single slash prefix.
	// And a target should not be supplied in addition to the UNC argument.
	// (This is allowed, but we want 1 or the other, not both.)
	target := host.Point
	if isUNC := strings.HasPrefix(target, uncPrefix); isUNC {
		return "", append(fuseArgs, uncOption(target))
	}
	return target, fuseArgs
}

func nameOptionSize(id filesystem.ID) int {
	var (
		name    = string(id)
		nameLen = len(name)
	)
	if nameLen == 0 {
		return 0
	}
	var (
		sysSize = len(systemNameOpt) + nameLen
		volSize = len(volNameOpt) + nameLen
	)
	return sysSize + delimiterSize + volSize
}

func nameOption(b *strings.Builder, id filesystem.ID) {
	name := string(id)
	b.WriteString(systemNameOpt)
	b.WriteString(name)
	b.WriteRune(optionDelimiter)
	b.WriteString(volNameOpt)
	b.WriteString(name)
}

func uncOption(target string) string {
	var option strings.Builder
	option.Grow(len(volumeOpt) + len(target) - 1)
	option.WriteString(volumeOpt)
	option.WriteString(target[1:])
	return option.String()
}

func getOSTarget(target string, args []string) string {
	if target != "" || len(args) == 0 {
		return target
	}
	var fromArg string
	for _, arg := range args {
		if strings.HasPrefix(arg, volumeOpt) {
			uncPath := arg[len(volumeOpt):]
			// The flag's parameter may be quoted but it's not required.
			// If it is, unwrap it.
			if raw, err := strconv.Unquote(uncPath); err == nil {
				uncPath = raw
			}
			// WinFSP uses a single separator for UNC in its
			// flag parameter; add a slash to create a valid system path.
			fromArg = string(os.PathSeparator) + uncPath
		}
	}
	return fromArg
}
