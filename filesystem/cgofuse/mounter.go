//go:build !nofuse
// +build !nofuse

package cgofuse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"runtime"
	"strings"
	"time"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/multiformats/go-multiaddr"
)

type (
	// mounter mounts requests in the host FS via the Fuse API
	mounter struct {
		ctx  context.Context
		fuse fuselib.FileSystemInterface
		fsid filesystem.ID
	}
	mountpoint struct {
		target multiaddr.Multiaddr
		io.Closer
	}

	closer func() error
)

func (close closer) Close() error { return close() }

func (m *mountpoint) Target() multiaddr.Multiaddr { return m.target }

func NewMounter(ctx context.Context, fileSystem fs.FS) (filesystem.Mounter, error) {
	fsi, err := NewFuseInterface(fileSystem)
	if err != nil {
		return nil, err
	}

	var fsid filesystem.ID
	if identifier, ok := fileSystem.(filesystem.IdentifiedFS); ok {
		fsid = identifier.ID()
	}

	return &mounter{
		ctx:  ctx,
		fuse: fsi,
		fsid: fsid,
	}, nil
}

func (m *mounter) Mount(ctx context.Context, target multiaddr.Multiaddr) (filesystem.MountPoint, error) {

	// NOTE: We don't use target.ValueForProtocol
	// because it processes the value, rather than returning it raw.
	// (For filepaths this can cause issues on non-Unix systems)

	targetPath, err := target.ValueForProtocol(filesystem.PathProtocol)
	if err != nil {
		// TODO: wrap error with info "need path, something something"
		return nil, err
	}

	closer, err := attachToHost(m.fuse, m.fsid, targetPath)
	if err != nil {
		return nil, err
	}

	return &mountpoint{
		target: target,
		Closer: closer,
	}, nil
}

// TODO: [Ame] English.
//
// `fsi.Mount` will panic before calling `fsi.Init` if the fuse libraries are not found.
// We want to recover from this and return that error (instead of exiting the process).
// Mount  may also return `false` if a non-fatal issue is encountered.
// (typical errors are lack of support, permission, a duplicate target, etc.
// These are logged to the console, but not returned to us via the API)
//
// If everything is okay, we expect `fsi.Mount` to block until a call to `fsi.Unmount`.
//
// errChan should be buffered. A single error will be sent only if mount fails,
// otherwise no value is sent.
func safeMount(hostInterface *fuselib.FileSystemHost, fsid filesystem.ID,
	target string, errChan chan<- error) {
	go func() {
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

		// TODO: we should parse the arguments here, as late as possible.
		// ^ leave a note explaining why - target may be empty, moving into the args (UNC paths do this)
		// but we want the raw value in this scope at least for the error message.

		// TODO: [port] hasty hacks right now
		var fuseArgs []string
		if runtime.GOOS == "windows" {
			var (
				opts  = "uid=-1,gid=-1"
				isUNC = len(target) > 2 &&
					(target)[:2] == `\\`
			)
			if fsid != 0 {
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
			} else {
				// TODO: reconsider where to do this
				// Multiaddr inserts this into our `path` protocol values
				target = strings.TrimPrefix(target, "/")
			}
		}

		if !hostInterface.Mount(target, fuseArgs) {
			errChan <- fmt.Errorf("%s: mount failed for an unknown reason", target)
		}
	}()
}

// NOTE: [b7952c54-1614-45ea-a042-7cfae90c5361] cgofuse only supports ReaddirPlus on Windows
// if this ever changes (bumps libfuse from 2.8 -> 3.X+), add platform support here (and to any other tags with this UUID)
// TODO: this would be best in the fuselib itself; make a patch upstream
const canReaddirPlus bool = runtime.GOOS == "windows"

func attachToHost(fsi fuselib.FileSystemInterface, fsid filesystem.ID, target string) (io.Closer, error) {
	hostInterface := fuselib.NewFileSystemHost(fsi)
	hostInterface.SetCapReaddirPlus(canReaddirPlus)
	hostInterface.SetCapCaseInsensitive(false)

	errChan := make(chan error, 1)
	safeMount(hostInterface, fsid, target, errChan)

	// This is how long we'll wait for `Mount` to fail
	// before assume it's running as expected (blocking forever).
	const failureThreasehold = 200 * time.Millisecond
	select {
	case err := <-errChan:
		return nil, err
	case <-time.After(failureThreasehold):
		// `Mount` hasn't panicked or returned an error yet
		// assume `Mount` is running forever (as intended)
	}

	/* TODO: convert this - we'll likely need to take options in the constructor
	something like:
	NewMounter(..., WithIndex(env.index))
	+
	`fsi.(someIntf).OnDestroy(index.Remove(self)`
	Old:
		if ffs, ok := fuseFS.(*hostBinding); ok {
			ffs.destroySignal = make(fuseMountSignal) // NOTE: expect this to be nil after calling `Unmount`
		}
	*/

	instanceDetach := closer(func() error {
		// TODO: feed close errors back to constructor caller
		// ^ the service daemon's logger provides an error channel
		// we need to pass it down to here.

		if !hostInterface.Unmount() {
			return fmt.Errorf("%s: unmount failed for an unknown reason", target)
		}
		return nil
	})

	return instanceDetach, nil
}
