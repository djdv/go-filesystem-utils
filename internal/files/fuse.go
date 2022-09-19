package files

import (
	"io"
	"sync/atomic"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
)

type (
	FuseDir struct {
		p9.File
		path *atomic.Uint64
	}
	fuseInterfaceFile struct {
		templatefs.NoopFile
		mountpoint io.Closer
		metadata
	}
)

func NewFuseDir(options ...MetaOption) (p9.QID, *FuseDir) {
	var (
		qid, dir = NewDirectory(options...)
		fsys     = &FuseDir{File: dir, path: dir.path}
	)
	dir.RDev = p9.Dev(filesystem.Fuse)
	return qid, fsys
}

func (dir *FuseDir) Mkdir(name string, permissions p9.FileMode, _ p9.UID, gid p9.GID) (p9.QID, error) {
	fsid, err := filesystem.ParseID(name)
	if err != nil {
		return p9.QID{}, err
	}
	want := p9.AttrMask{UID: true}
	attr, err := getAttrs(dir.File, want)
	if err != nil {
		return p9.QID{}, err
	}

	var ( // TODO: Proper.
		qid, fsiDir = NewFSIDDir(fsid, WithPath(dir.path))
		eDir        = &ephemeralDir{
			File:   fsiDir,
			parent: dir,
			name:   name,
			path:   dir.path,
		}
	) //
	const withServerTimes = true
	if err := setAttr(eDir, &p9.Attr{
		Mode: (permissions.Permissions() &^ S_LINMSK) & S_IRWXA,
		UID:  attr.UID,
		GID:  gid,
	}, withServerTimes); err != nil {
		return qid, err
	}
	return qid, dir.Link(eDir, name)
}

func createFuseInterfaceFile(closer io.Closer, name string, permissions p9.FileMode,
	parent p9.File, path *atomic.Uint64,
	uid p9.UID, gid p9.GID,
) (p9.QID, error) {
	var (
		interfaceFile, err = makeFuseInterfaceFile(closer,
			permissions, uid, gid, WithPath(path))
		qid = *interfaceFile.QID
	)
	if err != nil {
		return qid, err
	}
	return qid, parent.Link(interfaceFile, name)
}

func makeFuseInterfaceFile(closer io.Closer,
	permissions p9.FileMode, uid p9.UID, gid p9.GID,
	options ...MetaOption,
) (*fuseInterfaceFile, error) {
	interfaceFile := &fuseInterfaceFile{
		metadata:   makeMetadata(p9.ModeRegular, options...),
		mountpoint: closer,
	}
	const withServerTimes = true
	return interfaceFile, setAttr(interfaceFile, &p9.Attr{
		Mode: permissions,
		UID:  uid,
		GID:  gid,
		// TODO: sizeof json render of fif
		// Size: uint64(len(listener.Multiaddr().Bytes())),
	}, withServerTimes)
}

func (fsi *fuseInterfaceFile) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	return fsi.metadata.SetAttr(valid, attr)
}

func (fsi *fuseInterfaceFile) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return fsi.metadata.GetAttr(req)
}
