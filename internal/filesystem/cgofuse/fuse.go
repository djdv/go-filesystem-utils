//go:build !nofuse

package cgofuse

import (
	"fmt"
	"io"
	"io/fs"
	"math"
	"sync"

	"github.com/winfsp/cgofuse/fuse"
)

type (
	errNo           = int
	fileDescriptor  = uint64
	fileType        = uint32
	filePermissions = uint32
	id              = uint32
	uid             = id
	gid             = id
	Fuse            struct {
		*fuse.FileSystemHost
	}
	fuseContext struct {
		uid
		gid
		// NOTE: PID omitted as not used.
	}
	fileHandle struct {
		ioMu   sync.Mutex // TODO: name and responsibility; currently applies to the position cursor
		goFile fs.File
	}
	seekerFile interface {
		fs.File
		io.Seeker
	}
)

const (
	posixRoot = "/"
	// posixOmittedID shall be used as a special sentinel value,
	// to distinguish between Go's zero value for integers (0),
	// and an explicitly "unset" value.
	// This value was chosen as it should be reserved
	// (and thus never used for a real ID) in the `chown` syscall.
	// Reference: SUSv4BSi7
	posixOmittedID id = math.MaxUint32

	operationSuccess = 0

	// SUSv4BSi7 permission bits
	// extended and aliased
	// for Go style conventions.

	executeOther = fuse.S_IXOTH
	writeOther   = fuse.S_IWOTH
	readOther    = fuse.S_IROTH

	executeGroup = fuse.S_IXGRP
	writeGroup   = fuse.S_IWGRP
	readGroup    = fuse.S_IRGRP

	executeUser = fuse.S_IXUSR
	writeUser   = fuse.S_IWUSR
	readUser    = fuse.S_IRUSR

	executeAll = executeUser | executeGroup | executeOther
	writeAll   = writeUser | writeGroup | writeOther
	readAll    = readUser | readGroup | readOther

	allOther = readOther | writeOther | executeOther
	allGroup = readGroup | writeGroup | executeGroup
	allUser  = readUser | writeUser | executeUser
)

func (fh Fuse) Close() error {
	if !fh.Unmount() {
		// TODO: we should store the target + whatever else
		// so we can print out a more helpful message here.
		// TODO: investigate forking fuse so that it returns us the same error
		// it throws to the system's logger.
		return fmt.Errorf("unmount failed - system log may have more information")
	}
	return nil
}
