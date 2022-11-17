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
	errNo          = int
	fileDescriptor = uint64
	fileType       = uint32
	id             = uint32
	uid            = id
	gid            = id
	Fuse           struct {
		*fuse.FileSystemHost
	}
	fileHandle struct {
		goFile fs.File
		ioMu   sync.Mutex // TODO: name and responsibility; currently applies to the position cursor
	}
	fileMap map[fileDescriptor]*fileHandle

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
	errorHandle      = math.MaxUint64
	handleMax        = errorHandle - 1

	S_IRWXO = fuse.S_IROTH | fuse.S_IWOTH | fuse.S_IXOTH
	S_IRWXG = fuse.S_IRGRP | fuse.S_IWGRP | fuse.S_IXGRP
	S_IRWXU = fuse.S_IRUSR | fuse.S_IWUSR | fuse.S_IXUSR

	IRWXA = S_IRWXU | S_IRWXG | S_IRWXO                           // 0o777
	IRXA  = IRWXA &^ (fuse.S_IWUSR | fuse.S_IWGRP | fuse.S_IWOTH) // 0o555
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
