//go:build !nofuse

package cgofuse

import (
	"io/fs"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/winfsp/cgofuse/fuse"
)

func dirStat(ent fs.DirEntry) (*fuse.Stat_t, error) {
	goStat, err := ent.Info()
	if err != nil {
		return nil, err
	}
	entStat := &fuse.Stat_t{
		Mode: goToFuseFileType(goStat.Mode()) |
			IRXA&^(fuse.S_IXOTH), // TODO: const permissions; used here and in getattr
		Uid:  posixOmittedID,
		Gid:  posixOmittedID,
		Size: goStat.Size(),
		Mtim: fuse.NewTimespec(goStat.ModTime()),
	}
	if posixStat, ok := goStat.(filesystem.POSIXInfo); ok {
		entStat.Atim = fuse.NewTimespec(posixStat.AccessTime())
		entStat.Ctim = fuse.NewTimespec(posixStat.ChangeTime())
		// TODO: Populate with full SUSv4;VSi7 set when interface is updated.
		// UIDs, etc.
	}
	if extendedStat, ok := goStat.(filesystem.CreationTimer); ok {
		entStat.Birthtim = fuse.NewTimespec(extendedStat.CreationTime())
	}

	// FIXME: We need to obtain these values during Opendir.
	// And assign them here.
	// They are NOT guaranteed to be valid within calls to Readdir.
	// (some platforms may return data here)
	// stat.Uid, stat.Gid, _ = fuselib.Getcontext()
	return entStat, nil
}
