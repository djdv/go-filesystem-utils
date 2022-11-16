package cgofuse

import (
	"fmt"
	"math"

	"github.com/winfsp/cgofuse/fuse"
	fuselib "github.com/winfsp/cgofuse/fuse"
)

const (
	posixRoot = "/"

	operationSuccess = 0
	errorHandle      = math.MaxUint64
	handleMax        = errorHandle - 1

	S_IRWXO = fuselib.S_IROTH | fuselib.S_IWOTH | fuselib.S_IXOTH
	S_IRWXG = fuselib.S_IRGRP | fuselib.S_IWGRP | fuselib.S_IXGRP
	S_IRWXU = fuselib.S_IRUSR | fuselib.S_IWUSR | fuselib.S_IXUSR

	IRWXA = S_IRWXU | S_IRWXG | S_IRWXO                                    // 0o777
	IRXA  = IRWXA &^ (fuselib.S_IWUSR | fuselib.S_IWGRP | fuselib.S_IWOTH) // 0o555
)

type (
	errNo        = int
	fuseFileType = uint32
	Fuse         struct {
		*fuse.FileSystemHost
	}
)

func (fh Fuse) Close() error {
	if !fh.Unmount() {
		// TODO: we should store the target + whatever else
		// so we can print out a more helpful message here.
		// TODO: investigate forking fuselib so that it returns us the same error
		// it throws to the system's logger.
		return fmt.Errorf("unmount failed - system log may have more information")
	}
	return nil
}
