package cgofuse

import (
	"io/fs"

	"github.com/winfsp/cgofuse/fuse"
)

func dirStat(ent fs.DirEntry) *fuse.Stat_t {
	goStat, err := ent.Info()
	if err != nil {
		gw.log.Print(err)
		return nil
	}
	mTime := fuse.NewTimespec(goStat.ModTime())

	// FIXME: We need to obtain these values during Opendir.
	// And assign them here.
	// They are NOT guaranteed to be valid within calls to Readdir.
	// (some platforms may return data here)
	// stat.Uid, stat.Gid, _ = fuselib.Getcontext()
	return &fuse.Stat_t{
		Mode: goToFuseFileType(goStat.Mode()) |
			IRXA&^(fuse.S_IXOTH), // TODO: const permissions; used here and in getattr
		Uid:      dir.uid,
		Gid:      dir.gid,
		Size:     goStat.Size(),
		Atim:     mTime,
		Mtim:     mTime,
		Ctim:     mTime,
		Birthtim: mTime,
	}
}
