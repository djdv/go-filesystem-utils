//go:build !nofuse
// +build !nofuse

package cgofuse

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

type closer func() error

func (close closer) Close() error { return close() }

// NOTE: [b7952c54-1614-45ea-a042-7cfae90c5361] cgofuse only supports ReaddirPlus on Windows
// if this ever changes (bumps libfuse from 2.8 -> 3.X+), add platform support here (and to any other tags with this UUID)
// TODO: this would be best in the fuselib itself; make a patch upstream
const canReaddirPlus bool = runtime.GOOS == "windows"

// TODO: unexport this? Investigate callsite and maybe invert them.
/*
func AttachToHost(fsi fuselib.FileSystemInterface, fsid filesystem.ID, target string) (io.Closer, error) {
	hostInterface := fuselib.NewFileSystemHost(fsi)
	hostInterface.SetCapReaddirPlus(canReaddirPlus)
	hostInterface.SetCapCaseInsensitive(false)

	// This is how long we'll wait for `Mount` to fail
	// before assume it's running as expected (blocking forever).
	const failureThreasehold = 200 * time.Millisecond

	errChan := safeMount(hostInterface, fsid, target)
	select {
	case err := <-errChan:
		return nil, err
	case <-time.After(failureThreasehold):
		// `Mount` hasn't panicked or returned an error yet
		// assume `Mount` is running forever (as intended)
	}

	instanceDetach := closer(func() error {
		if !hostInterface.Unmount() {
			return fmt.Errorf("%s: unmount failed for an unknown reason", target)
		}
		return <-errChan
	})

	return instanceDetach, nil
}
*/

// TODO: this code an deeper is ancient and likely needs a redesign.
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
			errChan <- fmt.Errorf("%s: mount failed for an unknown reason", target)
		}
	}()
	return errChan
}
