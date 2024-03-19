//go:build !windows

package cgofuse

import (
	"io/fs"

	"github.com/winfsp/cgofuse/fuse"
)

func (fsys *fileSystem) Access(path string, mask uint32) errNo {
	defer fsys.systemLock.Access(path)()
	return fsys.access(path, mask)
}

// [2022.11.15] readdir-plus in cgofuse is only supported on Windows.
// If support for a system is added in cgofuse,
// metadata should be returned within `readdir` in this project as well.
// This function is a no-op since FUSE will use `getattr` instead
// to retrieve metadata on systems without the readdir-plus capability.
func dirStat(fs.DirEntry, fuseContext) (*fuse.Stat_t, error) { return nil, nil }
