//go:build !nofuse
// +build !nofuse

package cgofuse

import (
	fuselib "github.com/billziss-gh/cgofuse/fuse"
)

// metadata methods that don't apply to our systems

func (fs *hostBinding) Access(path string, mask uint32) int {
	fs.log.Printf("Access {%X}%q", mask, path)
	return -fuselib.ENOSYS
}

func (fs *hostBinding) Setxattr(path, name string, value []byte, flags int) int {
	fs.log.Printf("Setxattr {%X|%s|%d}%q", flags, name, len(value), path)
	return -fuselib.ENOSYS
}

func (fs *hostBinding) Getxattr(path, name string) (int, []byte) {
	fs.log.Printf("Getxattr {%s}%q", name, path)
	return -fuselib.ENOSYS, nil
}

func (fs *hostBinding) Removexattr(path, name string) int {
	fs.log.Printf("Removexattr {%s}%q", name, path)
	return -fuselib.ENOSYS
}

func (fs *hostBinding) Listxattr(path string, fill func(name string) bool) int {
	fs.log.Printf("Listxattr %q", path)
	return -fuselib.ENOSYS
}

// TODO: we could have these change for the entire system but that might be weird

func (fs *hostBinding) Chmod(path string, mode uint32) int {
	fs.log.Printf("Chmod {%X}%q", mode, path)
	return -fuselib.ENOSYS
}

func (fs *hostBinding) Chown(path string, uid, gid uint32) int {
	fs.log.Printf("Chown {%d|%d}%q", uid, gid, path)
	return -fuselib.ENOSYS
}

func (fs *hostBinding) Utimens(path string, tmsp []fuselib.Timespec) int {
	fs.log.Printf("Utimens {%v}%q", tmsp, path)
	return -fuselib.ENOSYS
}

// no hard links
func (fs *hostBinding) Link(oldpath, newpath string) int {
	fs.log.Printf("Link %q<->%q", oldpath, newpath)
	return -fuselib.ENOSYS
}

// syncing operations that generally don't apply if write operations don't apply
//  TODO: we need to utilize these for writable systems; ENOSYS for non writables

func (fs *hostBinding) Flush(path string, fh uint64) int {
	fs.log.Printf("Flush {%X}%q", fh, path)
	return -fuselib.ENOSYS
}

func (fs *hostBinding) Fsync(path string, datasync bool, fh uint64) int {
	fs.log.Printf("Fsync {%X|%t}%q", fh, datasync, path)
	return -fuselib.ENOSYS
}

func (fs *hostBinding) Fsyncdir(path string, datasync bool, fh uint64) int {
	fs.log.Printf("Fsyncdir {%X|%t}%q", fh, datasync, path)
	return -fuselib.ENOSYS
}
