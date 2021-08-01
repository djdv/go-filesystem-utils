//+build !nofuse

package cgofuse

import (
	fuselib "github.com/billziss-gh/cgofuse/fuse"
)

func (fs *hostBinding) Unlink(path string) int {
	fs.log.Debugf("Unlink - HostRequest %q", path)

	if path == "/" {
		fs.log.Error(fuselib.Error(-fuselib.EPERM))
		return -fuselib.EPERM
	}

	if err := fs.nodeInterface.Remove(path); err != nil {
		fs.log.Error(err)
		return interpretError(err)
	}

	return operationSuccess
}

func (fs *hostBinding) Rmdir(path string) int {
	fs.log.Debugf("Rmdir - HostRequest %q", path)

	if err := fs.nodeInterface.RemoveDirectory(path); err != nil {
		fs.log.Error(err)
		return interpretError(err)
	}

	return operationSuccess
}
