//go:build !nofuse && !(windows || plan9 || netbsd || openbsd)
// +build !nofuse,!windows,!plan9,!netbsd,!openbsd

package bazil

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"github.com/ipfs/go-ipfs/core"
	"github.com/ipfs/go-ipfs/core/commands/filesystem/bazil/ipns"
	rofs "github.com/ipfs/go-ipfs/core/commands/filesystem/bazil/readonly"
	"github.com/ipfs/go-ipfs/filesystem"
	"github.com/ipfs/go-ipfs/filesystem/manager"
	"github.com/ipfs/go-log"
)

// bazilBinder mounts requests in the host FS via the Fuse API
type bazilBinder struct {
	ctx context.Context
	*core.IpfsNode
	filesystem.ID

	fuseConn     *fuse.Conn
	mountOptions []fuse.MountOption
}

// TODO: docs; single file systems, direct requests
func NewBinder(ctx context.Context, fsid filesystem.ID, node *core.IpfsNode, allowOther bool) (manager.Binder, error) {
	var opts []fuse.MountOption
	if allowOther {
		opts = []fuse.MountOption{fuse.AllowOther()}
	}

	switch fsid {
	case filesystem.IPFS, // supported systems
		filesystem.IPNS:
	default: // unexpected request
		return nil, fmt.Errorf("no support for file system ID: %v", fsid)
	}

	return &bazilBinder{
		ctx:          ctx,
		IpfsNode:     node,
		ID:           fsid,
		mountOptions: opts,
	}, nil
}

func (ca *bazilBinder) Bind(ctx context.Context, requests manager.Requests) manager.Responses {
	// NOTE: [legacy]
	// if our subsystem is IPNS
	// we expect requests to come in sequence IPFS, IPNS, IPFS, IPNS, ...
	// we'll effectively block until we receive both of the pair (or `close(requests)/cancel` happens)
	// if a request is half full when requests are closed, this is treated as an error

	responses := make(chan manager.Response)
	go func() {
		defer close(responses)
		var ipfsMountpoint string       // holds the IPFS mountpoint string (which should be activated before host-fs use)
		for request := range requests { // ^ which is used in the actual IPNS mount call below
			var (
				mountpoint string // our request target
				err        error
				response   = manager.Response{Request: request}
			)

			if ca.ID == filesystem.IPNS && len(ipfsMountpoint) == 0 { // legacy hacks from above
				if ipfsMountpoint, err = request.ValueForProtocol(int(filesystem.PathProtocol)); err != nil {
					goto respond
				}
				if len(ipfsMountpoint) == 0 {
					err = fmt.Errorf("IPNS requests must be preceded by at least 1 valid IPFS mount request - empty path value for request: %#v", request)
					goto respond
				}
				continue // ipfsPath looks valid, proceed to the next request (will be interpreted as IPNS)
			}

			mountpoint, err = request.ValueForProtocol(int(filesystem.PathProtocol))
			if err != nil {
				goto respond
			}

			switch ca.ID {
			case filesystem.IPFS:
				response.Closer, err = fuseMount(mountpoint, "", ca.ID, ca.IpfsNode, ca.mountOptions...)
			case filesystem.IPNS:
				response.Closer, err = fuseMount(ipfsMountpoint, mountpoint, ca.ID, ca.IpfsNode, ca.mountOptions...)
				ipfsMountpoint = ipfsMountpoint[:0] // "consume"/invalidate this value
			}

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

type closer func() error      // io.Closer closure wrapper
func (f closer) Close() error { return f() }
func fuseMount(ipfsMountpoint, ipnsMountpoint string, fsid filesystem.ID, node *core.IpfsNode, opts ...fuse.MountOption) (instance closer, err error) {
	var (
		f            fs.FS
		fsMountpoint string
		logName      = strings.ToLower(path.Join("fuse", fsid.String()))
	)
	switch fsid {
	case filesystem.IPFS:
		f, err = rofs.NewFileSystem(node, log.Logger(logName))
		fsMountpoint = ipfsMountpoint
	case filesystem.IPNS:
		f, err = ipns.NewFileSystem(node, ipfsMountpoint, ipnsMountpoint, log.Logger(logName))
		fsMountpoint = ipnsMountpoint
	default:
		err = fmt.Errorf("we don't handle requests for %v", fsid)
	}
	if err != nil {
		return
	}

	var fuseConn *fuse.Conn
	if fuseConn, err = fuse.Mount(fsMountpoint, opts...); err != nil {
		return
	}

	// NOTE: [legacy] this whole block has been deprecated in fuselib
	// `fuseConn` is ready immediately after `Mount` and `MountError` is unused
	const mountTimeout = time.Second * 5
	errs := make(chan error, 1)
	go func() {
		if err := fs.Serve(fuseConn, f); err != nil {
			errs <- err
		}
	}()
	select {
	case <-fuseConn.Ready:
		err = fuseConn.MountError
	case <-time.After(mountTimeout):
		err = fmt.Errorf("mounting %s timed out", fsMountpoint)
	case err = <-errs:
		err = fmt.Errorf("mount returned early while serving: %w", err)
	}
	if err != nil {
		if closeErr := fuseConn.Close(); closeErr != nil {
			err = fmt.Errorf("%w - additionally a close error was encountered: %s", err, closeErr)
		}
	} else {
		instance = func() error { return fuse.Unmount(fsMountpoint) }
	}
	return
}
