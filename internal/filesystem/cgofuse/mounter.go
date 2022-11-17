//go:build !nofuse
// +build !nofuse

package cgofuse

import (
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fuselib "github.com/winfsp/cgofuse/fuse"
)

// TODO: types
// TODO: signature / interface may need to change. We're going to want extensions to FS,
// and we have to decide if we want to use Go standard FS form, or explicitly typed interfaces.
func MountFuse(fsys fs.FS, target string) (*Fuse, error) {
	fuse, err := GoToFuse(fsys)
	if err != nil {
		return nil, err
	}

	var fsid filesystem.ID
	// TODO: define this interface within [filesystem] pkg.
	if idFS, ok := fsys.(interface {
		ID() filesystem.ID
	}); ok {
		fsid = idFS.ID()
	}

	return fuse, AttachToHost(fuse.FileSystemHost, fsid, target)
}

// TODO: this code and deeper is ancient and likely needs a redesign.
func AttachToHost(hostInterface *fuselib.FileSystemHost, fsid filesystem.ID, target string) error {
	// This is how long we'll wait for `Mount` to fail
	// before assume it's running as expected (blocking forever).
	const failureThreasehold = 200 * time.Millisecond
	errChan := safeMount(hostInterface, fsid, target)
	select {
	case err := <-errChan:
		return err
	case <-time.After(failureThreasehold):
		// `Mount` hasn't panicked or returned an error yet
		// assume `Mount` is running forever (as intended)
		return nil
	}
}

func safeMount(host *fuselib.FileSystemHost, fsid filesystem.ID, target string) <-chan error {
	errChan := make(chan error, 1)
	go func() {
		defer close(errChan)
		defer func() {
			if r := recover(); r != nil {
				switch runtime.GOOS {
				case "windows":
					if typedR, ok := r.(string); ok && typedR == "cgofuse: cannot find winfsp" {
						errChan <- errors.New(
							"WinFSP(http://www.secfs.net/winfsp/) is required to mount on this platform, but it was not found",
						)
					}
				default:
					errChan <- fmt.Errorf("cgofuse panicked while attempting to mount: %v", r)
				}
			}
		}()
		// TODO: [port] hasty hacks right now
		var fuseArgs []string
		if runtime.GOOS == "windows" {
			// TODO: reconsider where to do this
			// Multiaddr inserts this into our `path` protocol values
			target = strings.TrimPrefix(target, "/")

			var (
				opts  = "uid=-1,gid=-1"
				isUNC = len(target) > 2 &&
					(target)[:2] == `\\`
			)
			if fsid != 0 { // TODO if 0 we should probably error, up-front.
				opts += fmt.Sprintf(
					",FileSystemName=%s,volname=%s",
					fsid.String(),
					fsid.String(),
				)
			}
			fuseArgs = []string{"-o", opts}

			if isUNC {
				// The UNC argument for cgo-fuse/WinFSP uses a single backslash prefix.
				// (`\` not `\\`)
				uncTarget := target[1:]
				target = "" // target should not be supplied in addition to UNC args
				// TODO: Double check docs for this ^ things may have changed.
				fuseArgs = append(fuseArgs, fmt.Sprintf(`--VolumePrefix=%s`, uncTarget))
			}
		}

		// DBG
		// fuseArgs = append(fuseArgs, "-d")
		// fuseArgs = []string{"-d"}

		// log.Println("calling fuse mount with args:", target, fuseArgs)
		if !host.Mount(target, fuseArgs) {
			errChan <- fmt.Errorf("failed to mount \"%s\" for an unknown reason - system log may have more information", target)
		}
	}()
	return errChan
}
