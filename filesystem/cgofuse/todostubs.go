package cgofuse

import fuselib "github.com/billziss-gh/cgofuse/fuse"

func (fs *hostBinding) Create(path string, flags int, mode uint32) (int, uint64) {
	fs.log.Warnf("Create - {%X|%X}%q", flags, mode, path)

	// TODO: why is fuselib passing us flags and what are they?
	// both FUSE and SUS predefine what they should be (to Open)

	//return hostBinding.open(path, fuselib.O_WRONLY|fuselib.O_CREAT|fuselib.O_TRUNC)

	// disabled until we parse relevant flags in open
	// fuse will do shenanigans to make this work
	return -fuselib.ENOSYS, errorHandle
}

func (fs *hostBinding) Mknod(path string, mode uint32, dev uint64) int {
	fs.log.Warnf("Mknod {%X|%d}%q", mode, dev, path)
	return -fuselib.ENOSYS
}

func (fs *hostBinding) Mkdir(path string, mode uint32) int {
	fs.log.Warnf("Mkdir {%X}%q", mode, path)
	return -fuselib.ENOSYS
}

func (fs *hostBinding) Symlink(target, newpath string) int {
	fs.log.Warnf("Symlink %q->%q", newpath, target)
	return -fuselib.ENOSYS
}

func (fs *hostBinding) Readlink(path string) (int, string) {
	fs.log.Warnf("Readlink - %q", path)
	switch path {
	case "/":
		fs.log.Warnf("Readlink - root path is an invalid Request")
		return -fuselib.EINVAL, ""

	case "":
		fs.log.Error("Readlink - empty Request")
		return -fuselib.ENOENT, ""
	}
	return operationSuccess, ""
}

func (fs *hostBinding) Rename(oldpath, newpath string) int {
	fs.log.Warnf("Rename %q->%q", oldpath, newpath)
	return -fuselib.ENOSYS
}

func (fs *hostBinding) Truncate(path string, size int64, fh uint64) int {
	fs.log.Warnf("Truncate {%X|%d}%q", fh, size, path)
	return -fuselib.ENOSYS
}

func (fs *hostBinding) Unlink(path string) int {
	fs.log.Warnf("Unlink %q", path)
	return -fuselib.ENOSYS
}

func (fs *hostBinding) Rmdir(path string) int {
	fs.log.Warnf("Rmdir %q", path)
	return -fuselib.ENOSYS
}

func (fs *hostBinding) Statfs(path string, stat *fuselib.Statfs_t) int {
	fs.log.Warnf("Statfs %q", path)
	return -fuselib.ENOSYS
}
