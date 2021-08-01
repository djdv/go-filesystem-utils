//go:build !nofuse && !(windows || plan9 || netbsd || openbsd)
// +build !nofuse,!windows,!plan9,!netbsd,!openbsd

package ipns

import (
	"context"
	"errors"
	"os"

	fuse "bazil.org/fuse"
	fs "bazil.org/fuse/fs"
	"github.com/ipfs/go-ipfs/core/commands/filesystem/bazil/log"
	dag "github.com/ipfs/go-merkledag"
	mfs "github.com/ipfs/go-mfs"
	ft "github.com/ipfs/go-unixfs"
)

// Directory is wrapper over an mfs directory to satisfy the fuse fs interface
type Directory struct {
	log log.Directory

	mfsDir *mfs.Directory
}

// Attr returns the attributes of a given node.
func (fuseDir *Directory) Attr(ctx context.Context, a *fuse.Attr) error {
	fuseDir.log.Debug("Directory Attr")
	a.Mode = os.ModeDir | 0555
	a.Uid = uint32(os.Getuid())
	a.Gid = uint32(os.Getgid())
	return nil
}

// Lookup performs a lookup under this node.
func (d *Directory) Lookup(ctx context.Context, name string) (fs.Node, error) {
	d.log.Lookup(ctx, name)
	child, err := d.mfsDir.Child(name)
	if err != nil {
		// todo: make this error more versatile.
		return nil, fuse.ENOENT
	}

	switch child := child.(type) {
	case *mfs.Directory:
		return &Directory{mfsDir: child, log: d.log}, nil
	case *mfs.File:
		return &Node{fi: child, log: log.Node(d.log)}, nil
	default:
		// NB: if this happens, we do not want to continue, unpredictable behaviour
		// may occur.
		panic("invalid type found under directory. programmer error.")
	}
}

// ReadDirAll reads the link structure as directory entries
func (dir *Directory) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	var entries []fuse.Dirent
	listing, err := dir.mfsDir.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, entry := range listing {
		dirent := fuse.Dirent{Name: entry.Name}

		switch mfs.NodeType(entry.Type) {
		case mfs.TDir:
			dirent.Type = fuse.DT_Dir
		case mfs.TFile:
			dirent.Type = fuse.DT_File
		}

		entries = append(entries, dirent)
	}

	if len(entries) > 0 {
		return entries, nil
	}
	return nil, fuse.ENOENT
}

func (d *Directory) Mkdir(ctx context.Context, req *fuse.MkdirRequest) (fs.Node, error) {
	child, err := d.mfsDir.Mkdir(req.Name)
	if err != nil {
		return nil, err
	}

	return &Directory{mfsDir: child, log: d.log}, nil
}

func (fuseDir *Directory) Create(ctx context.Context, req *fuse.CreateRequest, resp *fuse.CreateResponse) (fs.Node, fs.Handle, error) {
	// New 'empty' file
	nd := dag.NodeWithData(ft.FilePBData(nil, 0))
	err := fuseDir.mfsDir.AddChild(req.Name, nd)
	if err != nil {
		return nil, nil, err
	}

	child, err := fuseDir.mfsDir.Child(req.Name)
	if err != nil {
		return nil, nil, err
	}

	fi, ok := child.(*mfs.File)
	if !ok {
		return nil, nil, errors.New("child creation failed")
	}

	nodechild := &Node{fi: fi, log: log.Node(fuseDir.log)}

	fd, err := fi.Open(mfs.Flags{
		Read:  req.Flags.IsReadOnly() || req.Flags.IsReadWrite(),
		Write: req.Flags.IsWriteOnly() || req.Flags.IsReadWrite(),
		Sync:  true,
	})
	if err != nil {
		return nil, nil, err
	}

	return nodechild, &File{fi: fd}, nil
}

func (fuseDir *Directory) Remove(ctx context.Context, req *fuse.RemoveRequest) error {
	err := fuseDir.mfsDir.Unlink(req.Name)
	if err != nil {
		return fuse.ENOENT
	}
	return nil
}

// Rename implements NodeRenamer
func (fuseDir *Directory) Rename(ctx context.Context, req *fuse.RenameRequest, newDir fs.Node) error {
	cur, err := fuseDir.mfsDir.Child(req.OldName)
	if err != nil {
		return err
	}

	err = fuseDir.mfsDir.Unlink(req.OldName)
	if err != nil {
		return err
	}

	switch newDir := newDir.(type) {
	case *Directory:
		nd, err := cur.GetNode()
		if err != nil {
			return err
		}

		err = newDir.mfsDir.AddChild(req.NewName, nd)
		if err != nil {
			return err
		}
	case *Node:
		fuseDir.log.Error("Cannot move node into a file!")
		return fuse.EPERM
	default:
		fuseDir.log.Error("Unknown node type for rename target dir!")
		return errors.New("unknown fs node type")
	}
	return nil
}
