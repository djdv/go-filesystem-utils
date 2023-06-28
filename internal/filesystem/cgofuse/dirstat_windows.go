package cgofuse

import (
	"io/fs"

	"github.com/winfsp/cgofuse/fuse"
)

func dirStat(ent fs.DirEntry, fCtx fuseContext) (*fuse.Stat_t, error) {
	info, err := ent.Info()
	if err != nil {
		return nil, err
	}
	stat := new(fuse.Stat_t)
	goToFuseStat(info, fCtx, stat)
	return stat, nil
}
