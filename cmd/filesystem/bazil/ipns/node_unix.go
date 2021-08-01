//go:build !nofuse && !(windows || plan9 || netbsd || openbsd)
// +build !nofuse,!windows,!plan9,!netbsd,!openbsd

package ipns

import (
	"context"
	"fmt"
	"io"
	"os"

	fuse "bazil.org/fuse"
	fs "bazil.org/fuse/fs"
	"github.com/ipfs/go-ipfs/core/commands/filesystem/bazil/log"
	mfs "github.com/ipfs/go-mfs"
)

// File is wrapper over an mfs file to satisfy the fuse fs interface
type File struct {
	fi  mfs.FileDescriptor
	log log.File
}

// Node is the core object representing a Fuse file system tree node.
type Node struct {
	log log.Node

	fi *mfs.File
}

// Attr returns the attributes of a given node.
func (fi *Node) Attr(ctx context.Context, a *fuse.Attr) error {
	fi.log.Attr(ctx, a)
	size, err := fi.fi.Size()
	if err != nil {
		// In this case, the dag node in question may not be unixfs
		return fmt.Errorf("fuse/ipns: failed to get file.Size(): %w", err)
	}
	a.Mode = os.FileMode(0666)
	a.Size = uint64(size)
	a.Uid = uint32(os.Getuid())
	a.Gid = uint32(os.Getgid())
	return nil
}

func (fi *Node) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	fd, err := fi.fi.Open(mfs.Flags{
		Read:  req.Flags.IsReadOnly() || req.Flags.IsReadWrite(),
		Write: req.Flags.IsWriteOnly() || req.Flags.IsReadWrite(),
		Sync:  true,
	})
	if err != nil {
		return nil, err
	}

	if req.Flags&fuse.OpenTruncate != 0 {
		if req.Flags.IsReadOnly() {
			fi.log.Error("tried to open a readonly file with truncate")
			return nil, fuse.ENOTSUP
		}
		fi.log.Info("Need to truncate file!")
		err := fd.Truncate(0)
		if err != nil {
			return nil, err
		}
	} else if req.Flags&fuse.OpenAppend != 0 {
		fi.log.Info("Need to append to file!")
		if req.Flags.IsReadOnly() {
			fi.log.Error("tried to open a readonly file with append")
			return nil, fuse.ENOTSUP
		}

		_, err := fd.Seek(0, io.SeekEnd)
		if err != nil {
			fi.log.Error("seek reset failed: ", err)
			return nil, err
		}
	}

	return &File{fi: fd}, nil
}

func (fi *File) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
	return fi.fi.Close()
}

func (fi *File) Forget() {
	// TODO(steb): this seems like a place where we should be *uncaching*, not flushing.
	err := fi.fi.Flush()
	if err != nil {
		fi.log.Debug("forget file error: ", err)
	}
}

// Fsync flushes the content in the file to disk.
func (fi *Node) Fsync(ctx context.Context, req *fuse.FsyncRequest) error {
	// This needs to perform a *full* flush because, in MFS, a write isn't
	// persisted until the root is updated.
	errs := make(chan error, 1)
	go func() {
		errs <- fi.fi.Flush()
	}()
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (fi *File) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	_, err := fi.fi.Seek(req.Offset, io.SeekStart)
	if err != nil {
		return err
	}

	fisize, err := fi.fi.Size()
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	readsize := min(req.Size, int(fisize-req.Offset))
	n, err := fi.fi.CtxReadFull(ctx, resp.Data[:readsize])
	resp.Data = resp.Data[:n]
	return err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (fi *File) Write(ctx context.Context, req *fuse.WriteRequest, resp *fuse.WriteResponse) error {
	// TODO: at some point, ensure that WriteAt here respects the context
	wrote, err := fi.fi.WriteAt(req.Data, req.Offset)
	if err != nil {
		return err
	}
	resp.Size = wrote
	return nil
}

func (fi *File) Flush(ctx context.Context, req *fuse.FlushRequest) error {
	errs := make(chan error, 1)
	go func() {
		errs <- fi.fi.Flush()
	}()
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (fi *File) Setattr(ctx context.Context, req *fuse.SetattrRequest, resp *fuse.SetattrResponse) error {
	if req.Valid.Size() {
		cursize, err := fi.fi.Size()
		if err != nil {
			return err
		}
		if cursize != int64(req.Size) {
			err := fi.fi.Truncate(int64(req.Size))
			if err != nil {
				return err
			}
		}
	}
	return nil
}
