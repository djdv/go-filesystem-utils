package cgofuse

import (
	"io/fs"

	"github.com/winfsp/cgofuse/fuse"
)

func (fsys *fileSystem) Access(path string, mask uint32) errNo {
	defer fsys.systemLock.Access(path)()
	if deleteCheck := mask&fuse.DELETE_OK != 0; deleteCheck {
		for _, rule := range fsys.deleteAccess {
			if rule == path {
				return -fuse.EPERM
			}
		}
	}
	return fsys.access(path, mask&^fuse.DELETE_OK)
}

func dirStat(ent fs.DirEntry, fCtx fuseContext) (*fuse.Stat_t, error) {
	info, err := ent.Info()
	if err != nil {
		return nil, err
	}
	stat := new(fuse.Stat_t)
	goToFuseStat(info, fCtx, stat)
	return stat, nil
}
