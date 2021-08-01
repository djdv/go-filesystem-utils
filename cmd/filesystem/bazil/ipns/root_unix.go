//go:build !nofuse && !(windows || plan9 || netbsd || openbsd)
// +build !nofuse,!windows,!plan9,!netbsd,!openbsd

// package fuse/ipns implements a fuse filesystem that interfaces
// with ipns, the naming system for ipfs.
package ipns

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	fuse "bazil.org/fuse"
	fs "bazil.org/fuse/fs"
	cid "github.com/ipfs/go-cid"
	"github.com/ipfs/go-ipfs/core"
	"github.com/ipfs/go-ipfs/core/commands/filesystem/bazil/log"
	coreapi "github.com/ipfs/go-ipfs/core/coreapi"
	logging "github.com/ipfs/go-log"
	dag "github.com/ipfs/go-merkledag"
	mfs "github.com/ipfs/go-mfs"
	ft "github.com/ipfs/go-unixfs"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	options "github.com/ipfs/interface-go-ipfs-core/options"
	path "github.com/ipfs/interface-go-ipfs-core/path"
)

type (
	// FileSystem is the readwrite IPNS Fuse Filesystem.
	FileSystem struct {
		log log.FileSystem

		coreiface.CoreAPI
		RootNode *Root
	}
	// Root is the root object of the filesystem tree.
	Root struct {
		log log.Root

		Ipfs coreiface.CoreAPI
		Keys map[string]coreiface.Key

		// Used for symlinking into ipfs
		IpfsRoot  string
		IpnsRoot  string
		LocalDirs map[string]fs.Node
		Roots     map[string]*mfs.Root

		LocalLinks map[string]*Link
	}
)

// NewFileSystem constructs new fs using given core.IpfsNode instance.
func NewFileSystem(node *core.IpfsNode, ipfsPath, ipnsPath string, ipfsLog *logging.ZapEventLogger) (fs *FileSystem, err error) {
	if ipfsLog == nil {
		ipfsLog = logging.Logger("fuse/ipns")
	}

	if os.Getenv("IPFS_FUSE_DEBUG") != "" {
		fuse.Debug = func(msg interface{}) {
			ipfsLog.Debug(msg)
		}
	}

	var (
		ctx      = node.Context()
		core     coreiface.CoreAPI
		key      coreiface.Key
		fuseRoot *Root
	)

	defer func() {
		if err != nil {
			ipfsLog.Error(err)
		}
	}()

	var logFS log.FileSystem
	if logFS, err = log.NewFileSystem(ipfsLog); err != nil {
		return
	}

	if core, err = coreapi.NewCoreAPI(node); err != nil {
		return
	}
	if key, err = core.Key().Self(ctx); err != nil {
		return
	}
	if fuseRoot, err = CreateRoot(ctx, core,
		map[string]coreiface.Key{
			"local": key,
		},
		ipfsPath, ipnsPath,
		ipfsLog,
	); err != nil {
		return
	}

	fs = &FileSystem{
		CoreAPI:  core,
		RootNode: fuseRoot,
		log:      logFS,
	}
	return
}

// Root constructs the Root of the filesystem, a Root object.
func (f *FileSystem) Root() (fs.Node, error) { f.log.Root(); return f.RootNode, nil }

func (f *FileSystem) Destroy() {
	f.log.Destroy()
	err := f.RootNode.Close()
	if err != nil {
		f.log.Errorf("Error Shutting Down Filesystem: %s\n", err)
	}
}

func ipnsPubFunc(ipfs coreiface.CoreAPI, key coreiface.Key) mfs.PubFunc {
	return func(ctx context.Context, c cid.Cid) error {
		_, err := ipfs.Name().Publish(ctx, path.IpfsPath(c), options.Name.Key(key.Name()))
		return err
	}
}

func loadRoot(ctx context.Context, ipfs coreiface.CoreAPI, key coreiface.Key, ipfsLog logging.EventLogger) (*mfs.Root, fs.Node, error) {
	node, err := ipfs.ResolveNode(ctx, key.Path())
	switch err {
	case nil:
	case coreiface.ErrResolveFailed:
		node = ft.EmptyDirNode()
	default:
		return nil, nil, fmt.Errorf("looking up %s: %w", key.Path(), err)
	}

	pbnode, ok := node.(*dag.ProtoNode)
	if !ok {
		return nil, nil, dag.ErrNotProtobuf
	}

	root, err := mfs.NewRoot(ctx, ipfs.Dag(), pbnode, ipnsPubFunc(ipfs, key))
	if err != nil {
		return nil, nil, err
	}

	return root, &Directory{mfsDir: root.GetDirectory(), log: log.Directory{ipfsLog}}, nil
}

func CreateRoot(ctx context.Context, ipfs coreiface.CoreAPI, keys map[string]coreiface.Key, ipfspath, ipnspath string, ipfsLog logging.EventLogger) (*Root, error) {
	ldirs := make(map[string]fs.Node)
	roots := make(map[string]*mfs.Root)
	links := make(map[string]*Link)

	for alias, k := range keys {
		root, fsn, err := loadRoot(ctx, ipfs, k, ipfsLog)
		if err != nil {
			return nil, err
		}

		name := k.ID().String()

		roots[name] = root
		ldirs[name] = fsn

		// set up alias symlink
		links[alias] = &Link{
			Target: name,
			log:    ipfsLog,
		}
	}

	return &Root{
		Ipfs:       ipfs,
		IpfsRoot:   ipfspath,
		IpnsRoot:   ipnspath,
		Keys:       keys,
		LocalDirs:  ldirs,
		LocalLinks: links,
		Roots:      roots,
		log:        log.Root{EventLogger: ipfsLog},
	}, nil
}

// Attr returns file attributes.
func (r *Root) Attr(ctx context.Context, a *fuse.Attr) error {
	r.log.Attr(nil, nil)
	a.Mode = os.ModeDir | 0111 // -rw+x
	return nil
}

// Lookup performs a lookup under this node.
func (root *Root) Lookup(ctx context.Context, name string) (fs.Node, error) {
	root.log.Lookup(nil, name)
	switch name {
	case "mach_kernel", ".hidden", "._.":
		// Just quiet some log noise on OS X.
		return nil, fuse.ENOENT
	}

	if lnk, ok := root.LocalLinks[name]; ok {
		return lnk, nil
	}

	nd, ok := root.LocalDirs[name]
	if ok {
		switch nd := nd.(type) {
		case *Directory:
			return nd, nil
		case *Node:
			return nd, nil
		default:
			return nil, fuse.EIO
		}
	}

	// other links go through ipns resolution and are symlinked into the ipfs mountpoint
	ipnsName := "/ipns/" + name
	resolved, err := root.Ipfs.Name().Resolve(ctx, ipnsName)
	if err != nil {
		root.log.Warnf("ipns: namesys resolve error: %s", err)
		return nil, fuse.ENOENT
	}

	if resolved.Namespace() != "ipfs" {
		return nil, errors.New("invalid path from ipns record")
	}

	return &Link{
		Target: root.IpfsRoot + "/" + strings.TrimPrefix(resolved.String(), "/ipfs/"),
		log:    root.log,
	}, nil
}

func (r *Root) Close() error {
	for _, mr := range r.Roots {
		err := mr.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// Forget is called when the filesystem is unmounted. probably.
// see comments here: http://godoc.org/bazil.org/fuse/fs#FSDestroyer
func (r *Root) Forget() {
	r.log.Forget()
	err := r.Close()
	if err != nil {
		r.log.Error(err)
	}
}

// ReadDirAll reads a particular directory. Will show locally available keys
// as well as a symlink to the peerID key
func (r *Root) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	r.log.ReadDirAll(nil)

	var listing []fuse.Dirent
	for alias, k := range r.Keys {
		ent := fuse.Dirent{
			Name: k.ID().Pretty(),
			Type: fuse.DT_Dir,
		}
		link := fuse.Dirent{
			Name: alias,
			Type: fuse.DT_Link,
		}
		listing = append(listing, ent, link)
	}
	return listing, nil
}
