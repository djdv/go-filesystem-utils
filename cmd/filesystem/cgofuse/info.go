//+build !nofuse

package cgofuse

import (
	fuselib "github.com/billziss-gh/cgofuse/fuse"
	"github.com/ipfs/go-ipfs/filesystem"
)

func (fs *hostBinding) Getattr(path string, stat *fuselib.Stat_t, fh uint64) int {
	fs.log.Debugf("Getattr - {%X}%q", fh, path)

	if path == "" {
		fs.log.Error(fuselib.Error(-fuselib.ENOENT))
		return -fuselib.ENOENT
	}

	iStat, _, err := fs.nodeInterface.Info(path, filesystem.StatRequestAll)
	if err != nil {
		errNo := interpretError(err)
		if errNo != -fuselib.ENOENT { // don't flood the logs with "not found" errors
			fs.log.Error(err)
		}
		return errNo
	}

	var ids statIDGroup
	ids.uid, ids.gid, _ = fuselib.Getcontext()
	applyIntermediateStat(stat, iStat)
	applyCommonsToStat(stat, fs.filesWritable, fs.mountTimeGroup, ids)
	return operationSuccess
}
