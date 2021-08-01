package sys

import (
	"syscall"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
	"golang.org/x/sys/unix"
)

func Statfs(path string, fStatfs *fuselib.Statfs_t) (int, error) {
	sysStat := &unix.Statfs_t{}
	if err := unix.Statfs(path, sysStat); err != nil {
		if errno, ok := err.(syscall.Errno); ok {
			return int(errno), err
		}
		return -fuselib.EACCES, err
	}

	// NOTE: These values are ignored by cgofuse
	// but fsid might be incorrect on some platforms too
	fStatfs.Fsid = uint64(sysStat.F_fsid.Val[0])<<32 | uint64(sysStat.F_fsid.Val[1])
	fStatfs.Flag = uint64(sysStat.F_flags)

	fStatfs.Bsize = uint64(sysStat.F_bsize)
	fStatfs.Blocks = sysStat.F_blocks
	fStatfs.Bfree = sysStat.F_bfree
	fStatfs.Bavail = uint64(sysStat.F_bavail)
	fStatfs.Files = sysStat.F_files
	fStatfs.Ffree = uint64(sysStat.F_ffree)
	fStatfs.Frsize = uint64(sysStat.F_bsize)
	fStatfs.Namemax = uint64(sysStat.F_namemax)
	return exitSuccess, nil
}
