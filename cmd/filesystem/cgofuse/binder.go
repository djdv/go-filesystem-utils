//go:build !nofuse
// +build !nofuse

package cgofuse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"time"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
	"github.com/ipfs/go-ipfs/filesystem"
	"github.com/ipfs/go-ipfs/filesystem/manager"
)

// TODO: migrate the rest of this file

// cgofuseBinder mounts requests in the host FS via the Fuse API
type cgofuseBinder struct {
	ctx    context.Context
	goFs   filesystem.Interface
	fuseFs fuselib.FileSystemInterface
}

func NewBinder(ctx context.Context, fs filesystem.Interface) (manager.Binder, error) {
	fsi, err := NewFuseInterface(fs)
	if err != nil {
		return nil, err
	}
	return &cgofuseBinder{
		ctx:    ctx,
		goFs:   fs,
		fuseFs: fsi,
	}, nil
}

func (ca *cgofuseBinder) Bind(ctx context.Context, requests manager.Requests) manager.Responses {
	responses := make(chan manager.Response)
	go func() {
		defer close(responses)
		for request := range requests {
			var (
				oldRequest Request // TODO: migrate
				response   = manager.Response{Request: request}
			)
			target, err := request.ValueForProtocol(int(filesystem.PathProtocol))
			if err != nil {
				goto respond
			}
			oldRequest, err = ParseRequest(ca.goFs.ID(), target) // TODO: migrate
			if err != nil {
				goto respond
			}
			response.Closer, response.Error = attachToHost(ca.fuseFs, oldRequest)

		respond:
			if err != nil {
				response.Error = err
			}
			select {
			case responses <- response:
			case <-ctx.Done():
				return
			}
		}
	}()

	return responses
}

// NOTE: [b7952c54-1614-45ea-a042-7cfae90c5361] cgofuse only supports ReaddirPlus on Windows
// if this ever changes (bumps libfuse from 2.8 -> 3.X+), add platform support here (and to any other tags with this UUID)
// TODO: this would be best in the fuselib itself; make a patch upstream
const canReaddirPlus bool = runtime.GOOS == "windows"

// cgofuse will panic before calling `hostBinding.Init` if the fuse libraries are not found
// or it encounters some kind of fatal issue.
// We want to recover from this and return that error (instead of exiting the process)
// It may also return `false` if a non-fatal issue is encountered.
// (typical errors are lack of support or permission, a duplicate target, etc.
// These are logged to the console, but not returned to us)
// Otherwise we expect to block forever (on `Mount`)
func safeMount(hostInterface *fuselib.FileSystemHost, request Request, failFunc func(error)) {
	defer func() {
		if r := recover(); r != nil {
			switch runtime.GOOS {
			case "windows":
				if typedR, ok := r.(string); ok && typedR == "cgofuse: cannot find winfsp" {
					failFunc(errors.New("WinFSP(http://www.secfs.net/winfsp/) is required to mount on this platform, but it was not found"))
				}
			default:
				failFunc(fmt.Errorf("cgofuse panicked while attempting to mount: %v", r))
			}
		}
	}()

	if !hostInterface.Mount(request.HostPath, request.FuseArgs) {
		failFunc(fmt.Errorf("%s: mount failed for an unknown reason", request.String()))
	}
}

type closer func() error      // io.Closer closure wrapper
func (f closer) Close() error { return f() }
func attachToHost(fuseFS fuselib.FileSystemInterface, request Request) (instanceDetach io.Closer, err error) {
	hostInterface := fuselib.NewFileSystemHost(fuseFS)
	hostInterface.SetCapReaddirPlus(canReaddirPlus)
	hostInterface.SetCapCaseInsensitive(false)

	// TODO: rewrite this whole function

	ctx, cancel := context.WithCancel(context.Background()) // [async-1]
	//hostPath := request.hostTarget()

	go func() {
		// [async-1]
		// We spin a thread for `Mount` (via `safeMount`) and try to poll the host FS to see if the mount succeeded.
		// If we can stat the target, or `safeMount` doesn't call us back with an error (immediately),
		// we make the assumption that `Mount` is in the process of hosting the target (expected/success)
		semaphore := make(chan struct{}) // this semaphore is used to orchestrate this behavior within our thread
		defer cancel()                   // regardless of what happens in this thread, unblock the parent thread when we're done

		go safeMount(hostInterface, request, func(e error) {
			err = e
			close(semaphore)
		})

		const (
			pollRate    = 200 * time.Millisecond
			pollTimeout = 3 * pollRate
		)

		// poll the OS to see if `Mount` succeeded
		go func() {
			callTimeout := time.After(pollTimeout)
			for {
				select {
				case <-semaphore: // `Mount` failed early
					return // `err` was set by the routine above; semaphore should have been closed by the failure closure we passed to it

					// FIXME: this poll is pointless for most platforms since the target is likely to already exist native, before we mount on top of it
					// only namespace APIs like NT allows does this work correctly
					// we'll need to check system specific APIs to see if mounts are active
					// NT: possible alternative; check if target exists before Mount, if it does, store its metadata and compare it in a polling loop (if it changes(timestamps), assume that was caused by us)
					// *nix: check /proc/mounts if it exists, otherwise mtab, otherwise ??? (some kind of common fsevent library probably exists)
					/*
						case <-time.After(pollRate):
							if _, err := os.Stat(hostPath); err == nil { // if the path exists, mount succeeded
								close(semaphore)
								return
							}
							// if not, keep polling
					*/

				case <-callTimeout:
					// `Mount` hasn't panicked or returned an error yet
					// but we didn't see the target in the FS
					// best we can do is assume `Mount` is running forever (as intended)
					// `err` remains nil
					close(semaphore)
					return
				}
			}
		}()

		<-semaphore // [async-1] wait for the system to respond; setting `err` or not
		if err == nil {
			// if this interface is ours, set up the close channel
			/* Old
			if ffs, ok := fuseFS.(*hostBinding); ok {
				ffs.destroySignal = make(fuseMountSignal) // NOTE: expect this to be nil after calling `Unmount`
			}
			*/
		}

		return
	}()

	<-ctx.Done() // [async-1] wait for `hostInterface.Mount` to fail, run, or panic
	if err != nil {
		return
	}

	// TODO: make better
	// we need to remove ourselves from the index on fs.Destroy since FUSE may call fs.Destroy without us knowing
	// (like when WinFSP receives a sigint)
	// this means piping the index delete() all the way down to the FS.Destroy
	// otherwise we double close on shutdown/unmount
	// because FUSE closed the FS, but we were still tracking it in the FS manager
	instanceDetach = closer(func() (err error) {
		// TODO: feed close errors back to constructor caller
		// ^ old branch has (bad) code for this

		// otherwise just do default behaviour
		if !hostInterface.Unmount() {
			err = fmt.Errorf("%s: unmount failed for an unknown reason", request.String())
		}

		return
	})

	return
}
