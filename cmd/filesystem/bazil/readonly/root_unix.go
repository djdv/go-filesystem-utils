//go:build !nofuse && !(windows || plan9 || netbsd || openbsd)
// +build !nofuse,!windows,!plan9,!netbsd,!openbsd

package readonly

import (
	"context"
	"os"

	fuse "bazil.org/fuse"
	fs "bazil.org/fuse/fs"
	core "github.com/ipfs/go-ipfs/core"
	"github.com/ipfs/go-ipfs/core/commands/filesystem/bazil/log"
	logging "github.com/ipfs/go-log"
	mdag "github.com/ipfs/go-merkledag"
	path "github.com/ipfs/go-path"
)

type (
	// FileSystem is the readonly IPFS Fuse Filesystem.
	FileSystem struct {
		log log.FileSystem

		*core.IpfsNode
	}

	// Root is the root object of the filesystem tree.
	Root struct {
		log log.Root

		*core.IpfsNode
	}
)

// NewFileSystem constructs new fs using given core.IpfsNode instance.
func NewFileSystem(node *core.IpfsNode, ipfsLog *logging.ZapEventLogger) (fs *FileSystem, err error) {
	if ipfsLog == nil {
		ipfsLog = logging.Logger("fuse/ipfs")
	}

	if os.Getenv("IPFS_FUSE_DEBUG") != "" {
		fuse.Debug = func(msg interface{}) {
			ipfsLog.Debug(msg)
		}
	}

	var logFS log.FileSystem
	if logFS, err = log.NewFileSystem(ipfsLog); err == nil {
		fs = &FileSystem{
			IpfsNode: node,
			log:      logFS,
		}
	}
	return
}

// Root constructs the Root of the filesystem, a Root object.
func (f *FileSystem) Root() (fs.Node, error) {
	f.log.Root()
	return &Root{
		IpfsNode: f.IpfsNode,
		log:      log.Root(f.log),
	}, nil
}

// Attr returns file attributes.
func (r *Root) Attr(ctx context.Context, a *fuse.Attr) (err error) {
	r.log.Attr(nil, nil)
	a.Mode = os.ModeDir | 0111 // -rw+x
	return
}

// Lookup performs a lookup under this node.
func (r *Root) Lookup(ctx context.Context, name string) (fs.Node, error) {
	r.log.Lookup(nil, name)
	switch name {
	case "mach_kernel", ".hidden", "._.": // Just quiet some log noise on OS X.
		return nil, fuse.ENOENT
	}

	p, err := path.ParsePath(name)
	if err != nil {
		r.log.Debugf("fuse failed to parse path: %q: %s", name, err)
		return nil, fuse.ENOENT
	}

	nd, err := r.Resolver.ResolvePath(ctx, p)
	if err != nil {
		// todo: make this error more versatile.
		return nil, fuse.ENOENT
	}

	switch nd := nd.(type) {
	case *mdag.ProtoNode, *mdag.RawNode:
		return &Node{
			Ipfs: (*core.IpfsNode)(r.IpfsNode),
			Nd:   nd,
			log:  log.Node(r.log),
		}, nil
	default:
		r.log.Error("fuse node was not a protobuf node")
		return nil, fuse.ENOTSUP
	}
}

// ReadDirAll reads a particular directory. Disallowed for root.
func (r *Root) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	r.log.ReadDirAll(nil)
	return nil, fuse.EPERM
}
