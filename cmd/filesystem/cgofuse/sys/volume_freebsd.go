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
	fStatfs.Fsid = uint64(sysStat.Fsid.Val[0])<<32 | uint64(sysStat.Fsid.Val[1])
	fStatfs.Flag = uint64(sysStat.Flags)

	fStatfs.Bsize = sysStat.Bsize
	fStatfs.Blocks = sysStat.Blocks
	fStatfs.Bfree = sysStat.Bfree
	fStatfs.Bavail = uint64(sysStat.Bavail)
	fStatfs.Files = sysStat.Files
	fStatfs.Ffree = uint64(sysStat.Ffree)
	fStatfs.Frsize = uint64(sysStat.Bsize)
	fStatfs.Namemax = uint64(sysStat.Namemax)
	return exitSuccess, nil
}
