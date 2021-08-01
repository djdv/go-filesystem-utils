// +build !windows,!darwin,!freebsd,!openbsd,!netbsd,!linux

package sys

import (
	fuselib "github.com/billziss-gh/cgofuse/fuse"
)

func Statfs(string, *fuselib.Statfs_t) (int, error) {
	return -fuselib.ENOSYS, nil
}
