//go:build !nofuse && !windows

package cgofuse

import "github.com/djdv/go-filesystem-utils/internal/filesystem"

const (
	cgoDepPanic   = "cgofuse: cannot find FUSE"
	cgoDepMessage = "A FUSE compatible library is required" +
		"to mount on this platform, but it was not found" +
		"\nSee: https://github.com/winfsp/cgofuse#readme"
)

func makeFuseArgs(fsid filesystem.ID, target string) (string, []string) {
	return target, nil
}
