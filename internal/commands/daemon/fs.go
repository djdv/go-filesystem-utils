package daemon

import (
	"context"
	"sync/atomic"

	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/p9/p9"
)

type fileSystem struct {
	path    ninePath
	root    p9.File
	mount   mountSubsystem
	listen  listenSubsystem
	control controlSubsystem
}

// Servers hosting the file system API
// should use these values when linking files into the system.
// Clients that interact with the [p9.File] services,
// should use these values to resolve them via `Walk`
// (from the server's 9P root).
const (
	// MountsFileName refers to the directory used
	// to store host and guest directories,
	// the latter of which will store [p9fs.MountFile] files.
	// E.g. `/mounts/FUSE/IPFS/mountpoint`.
	MountsFileName = "mounts"

	// ListenersFileName refers to the directory used
	// to store [p9fs.Listener].
	ListenersFileName = "listeners"

	// ControlFileName refers to the directory used
	// to store various server control files.
	ControlFileName = "control"

	// ShutdownFileName refers to the control file
	// used to request shutdown, by writing a
	// [shutdown.Disposition] (string or byte)
	// value to the file.
	ShutdownFileName = "shutdown"
)

func newFileSystem(ctx context.Context, uid p9.UID, gid p9.GID) (fileSystem, error) {
	const permissions = p9fs.ReadUser | p9fs.WriteUser | p9fs.ExecuteUser |
		p9fs.ReadGroup | p9fs.ExecuteGroup |
		p9fs.ReadOther | p9fs.ExecuteOther
	var (
		path         = new(atomic.Uint64)
		_, root, err = p9fs.NewDirectory(
			p9fs.WithPath[p9fs.DirectoryOption](path),
			p9fs.WithUID[p9fs.DirectoryOption](uid),
			p9fs.WithGID[p9fs.DirectoryOption](gid),
			p9fs.WithPermissions[p9fs.DirectoryOption](permissions),
			p9fs.WithoutRename[p9fs.DirectoryOption](true),
		)
	)
	if err != nil {
		return fileSystem{}, err
	}
	mount, err := newMounter(root, path, uid, gid, permissions)
	if err != nil {
		return fileSystem{}, err
	}
	listen, err := newListener(ctx, root, path, uid, gid, permissions)
	if err != nil {
		return fileSystem{}, err
	}
	control, err := newControl(ctx, root, path, uid, gid, permissions)
	if err != nil {
		return fileSystem{}, err
	}
	system := fileSystem{
		path:    path,
		root:    root,
		mount:   mount,
		listen:  listen,
		control: control,
	}
	return system, linkSystems(&system)
}

func newControl(ctx context.Context,
	parent p9.File, path ninePath,
	uid p9.UID, gid p9.GID, permissions p9.FileMode,
) (controlSubsystem, error) {
	_, control, err := p9fs.NewDirectory(
		p9fs.WithParent[p9fs.DirectoryOption](parent, ControlFileName),
		p9fs.WithPath[p9fs.DirectoryOption](path),
		p9fs.WithUID[p9fs.DirectoryOption](uid),
		p9fs.WithGID[p9fs.DirectoryOption](gid),
		p9fs.WithPermissions[p9fs.DirectoryOption](permissions),
		p9fs.WithoutRename[p9fs.DirectoryOption](true),
	)
	if err != nil {
		return controlSubsystem{}, err
	}
	var (
		sCtx, cancel    = context.WithCancel(ctx)
		filePermissions = permissions ^ (p9fs.ExecuteOther | p9fs.ExecuteGroup | p9fs.ExecuteUser)
	)
	_, shutdownFile, shutdownCh, err := p9fs.NewChannelFile(sCtx,
		p9fs.WithParent[p9fs.ChannelOption](control, ShutdownFileName),
		p9fs.WithPath[p9fs.ChannelOption](path),
		p9fs.WithUID[p9fs.ChannelOption](uid),
		p9fs.WithGID[p9fs.ChannelOption](gid),
		p9fs.WithPermissions[p9fs.ChannelOption](filePermissions),
	)
	if err != nil {
		cancel()
		return controlSubsystem{}, err
	}
	if err := control.Link(shutdownFile, ShutdownFileName); err != nil {
		cancel()
		return controlSubsystem{}, err
	}
	return controlSubsystem{
		name:      ControlFileName,
		directory: control,
		shutdown: shutdown{
			ChannelFile: shutdownFile,
			name:        ShutdownFileName,
			ch:          shutdownCh,
			cancel:      cancel,
		},
	}, nil
}

func linkSystems(system *fileSystem) error {
	root := system.root
	for _, file := range []struct {
		p9.File
		name string
	}{
		{
			name: system.mount.name,
			File: system.mount.MountFile,
		},
		{
			name: system.listen.name,
			File: system.listen.Listener,
		},
		{
			name: system.control.name,
			File: system.control.directory,
		},
	} {
		if err := root.Link(file.File, file.name); err != nil {
			return err
		}
	}
	return nil
}

func hasEntries(fsys p9.File) (bool, error) {
	ents, err := p9fs.ReadDir(fsys)
	if err != nil {
		return false, err
	}
	return len(ents) > 0, nil
}
