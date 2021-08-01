//go:build !nofuse && !(windows || plan9 || netbsd || openbsd)
// +build !nofuse,!windows,!plan9,!netbsd,!openbsd

package readonly

import (
	"context"
	"fmt"
	"io"
	"os"
	"syscall"

	fuse "bazil.org/fuse"
	fs "bazil.org/fuse/fs"
	core "github.com/ipfs/go-ipfs/core"
	"github.com/ipfs/go-ipfs/core/commands/filesystem/bazil/log"
	ipld "github.com/ipfs/go-ipld-format"
	mdag "github.com/ipfs/go-merkledag"
	ft "github.com/ipfs/go-unixfs"
	uio "github.com/ipfs/go-unixfs/io"
)

// Node is the core object representing a Fuse file system tree node.
type Node struct {
	log log.Node

	Ipfs   *core.IpfsNode
	Nd     ipld.Node
	cached *ft.FSNode
}

func (n *Node) loadData() error {
	if pbnd, ok := n.Nd.(*mdag.ProtoNode); ok {
		fsn, err := ft.FSNodeFromBytes(pbnd.Data())
		if err != nil {
			return err
		}
		n.cached = fsn
	}
	return nil
}

// Attr returns the attributes of a given node.
func (n *Node) Attr(ctx context.Context, a *fuse.Attr) error {
	n.log.Attr(ctx, a)
	if rawnd, ok := n.Nd.(*mdag.RawNode); ok {
		a.Mode = 0444
		a.Size = uint64(len(rawnd.RawData()))
		a.Blocks = 1
		return nil
	}

	if n.cached == nil {
		if err := n.loadData(); err != nil {
			return fmt.Errorf("readonly: loadData() failed: %s", err)
		}
	}
	switch n.cached.Type() {
	case ft.TDirectory, ft.THAMTShard:
		a.Mode = os.ModeDir | 0555
	case ft.TFile:
		size := n.cached.FileSize()
		a.Mode = 0444
		a.Size = uint64(size)
		a.Blocks = uint64(len(n.Nd.Links()))
	case ft.TRaw:
		a.Mode = 0444
		a.Size = uint64(len(n.cached.Data()))
		a.Blocks = uint64(len(n.Nd.Links()))
	case ft.TSymlink:
		a.Mode = 0777 | os.ModeSymlink
		a.Size = uint64(len(n.cached.Data()))
	default:
		return fmt.Errorf("invalid data type - %s", n.cached.Type())
	}
	return nil
}

// Lookup performs a lookup under this node.
func (n *Node) Lookup(ctx context.Context, name string) (fs.Node, error) {
	n.log.Lookup(ctx, name)
	link, _, err := uio.ResolveUnixfsOnce(ctx, n.Ipfs.DAG, n.Nd, []string{name})
	switch err {
	case os.ErrNotExist, mdag.ErrLinkNotFound:
		// todo: make this error more versatile.
		return nil, fuse.ENOENT
	default:
		n.log.Errorf("fuse lookup %q: %s", name, err)
		return nil, fuse.EIO
	case nil:
		// noop
	}

	nd, err := n.Ipfs.DAG.Get(ctx, link.Cid)
	switch err {
	case ipld.ErrNotFound:
	default:
		n.log.Errorf("fuse lookup %q: %s", name, err)
		return nil, err
	case nil:
		// noop
	}

	return &Node{Ipfs: n.Ipfs, Nd: nd, log: n.log}, nil
}

// ReadDirAll reads the link structure as directory entries
func (n *Node) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	n.log.Debug("Node ReadDir")
	dir, err := uio.NewDirectoryFromNode(n.Ipfs.DAG, n.Nd)
	if err != nil {
		return nil, err
	}

	var entries []fuse.Dirent
	err = dir.ForEachLink(ctx, func(lnk *ipld.Link) error {
		name := lnk.Name
		if len(name) == 0 {
			name = lnk.Cid.String()
		}
		nd, err := n.Ipfs.DAG.Get(ctx, lnk.Cid)
		if err != nil {
			n.log.Warn("error fetching directory child node: ", err)
		}

		t := fuse.DT_Unknown
		switch nd := nd.(type) {
		case *mdag.RawNode:
			t = fuse.DT_File
		case *mdag.ProtoNode:
			if fsn, err := ft.FSNodeFromBytes(nd.Data()); err != nil {
				n.log.Warn("failed to unmarshal protonode data field:", err)
			} else {
				switch fsn.Type() {
				case ft.TDirectory, ft.THAMTShard:
					t = fuse.DT_Dir
				case ft.TFile, ft.TRaw:
					t = fuse.DT_File
				case ft.TSymlink:
					t = fuse.DT_Link
				case ft.TMetadata:
					n.log.Error("metadata object in fuse should contain its wrapped type")
				default:
					n.log.Error("unrecognized protonode data type: ", fsn.Type())
				}
			}
		}
		entries = append(entries, fuse.Dirent{Name: name, Type: t})
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(entries) > 0 {
		return entries, nil
	}
	return nil, fuse.ENOENT
}

func (n *Node) Getxattr(ctx context.Context, req *fuse.GetxattrRequest, resp *fuse.GetxattrResponse) error {
	// TODO: is nil the right response for 'bug off, we ain't got none' ?
	resp.Xattr = nil
	return nil
}

func (n *Node) Readlink(ctx context.Context, req *fuse.ReadlinkRequest) (string, error) {
	if n.cached == nil || n.cached.Type() != ft.TSymlink {
		return "", fuse.Errno(syscall.EINVAL)
	}
	return string(n.cached.Data()), nil
}

func (n *Node) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	r, err := uio.NewDagReader(ctx, n.Nd, n.Ipfs.DAG)
	if err != nil {
		return err
	}
	_, err = r.Seek(req.Offset, io.SeekStart)
	if err != nil {
		return err
	}
	// Data has a capacity of Size
	buf := resp.Data[:int(req.Size)]
	readBytes, err := io.ReadFull(r, buf)
	resp.Data = buf[:readBytes]
	switch err {
	case nil, io.EOF, io.ErrUnexpectedEOF:
	default:
		return err
	}
	resp.Data = resp.Data[:readBytes]
	return nil // may be non-nil / not succeeded
}
