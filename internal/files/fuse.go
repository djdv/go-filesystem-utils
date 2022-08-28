package files

import (
	"io"
	"io/fs"
	"log"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
	"github.com/multiformats/go-multiaddr"
)

type (
	FuseDir struct{ *Directory }

	// TODO: unlink == fsh.unmount?
	fuseInterfaceFile struct {
		templatefs.NoopFile
		mountpoint io.Closer
		p9.Attr
		p9.QID
	}
)

func NewFuseDir(options ...DirectoryOption) *FuseDir {
	dir := &FuseDir{Directory: NewDirectory(options...)}
	dir.Directory.Attr.RDev = p9.Dev(filesystem.Fuse)
	return dir
}

func (dir *FuseDir) clone(withQID bool) ([]p9.QID, *FuseDir) {
	qids, dirClone := dir.Directory.clone(withQID)
	return qids, &FuseDir{Directory: dirClone}
}

func (dir *FuseDir) Walk(names []string) ([]p9.QID, p9.File, error) {
	return walk[*FuseDir](dir, names...)
}

func (dir *FuseDir) Mkdir(name string, permissions p9.FileMode, _ p9.UID, gid p9.GID) (p9.QID, error) {
	fsid, err := filesystem.ParseID(name)
	if err != nil {
		return p9.QID{}, err
	}
	if _, exists := dir.entries.load(name); exists {
		return p9.QID{}, perrors.EEXIST
	}
	fsidDir := NewFSIDDir(fsid,
		WithParent[DirectoryOption](dir),
		WithPath[DirectoryOption](dir.Directory.path),
	)
	if err := fsidDir.SetAttr(mkdirMask(permissions, dir.UID, gid)); err != nil {
		return *fsidDir.QID, err
	}
	return *fsidDir.QID, dir.Link(fsidDir, name)
}

func (dir *FuseDir) newHostFile(target string, fsid filesystem.ID, attr *p9.Attr) *fuseInterfaceFile {
	// TODO: return errors - don't panic
	// TODO:split up cases into functions

	var goFS fs.FS

	const maddr = `/ip4/127.0.0.1/tcp/5001`
	daemonMaddr := multiaddr.StringCast(maddr)
	// TODO [de-dupe]: convert PinFS to fallthrough to IPFS if possible.
	// Both need a client+IPFS-FS.
	switch fsid { // TODO: add all cases
	case filesystem.IPFS,
		filesystem.IPNS:
		client, err := ipfsClient(daemonMaddr)
		if err != nil {
			panic(err)
		}
		goFS = filesystem.NewIPFS(client, fsid)

	case filesystem.IPFSPins:
		client, err := ipfsClient(daemonMaddr)
		if err != nil {
			panic(err)
		}
		ipfs := filesystem.NewIPFS(client, filesystem.IPFS)
		goFS = filesystem.NewPinFS(client.Pin(), filesystem.WithIPFS(ipfs))
	case filesystem.IPFSKeys:
		goFS = filesystem.NewDBGFS() // FIXME: dbg fs for testing - remove.
	default:
		panic("not supported yet")
	}

	const dbgLog = false // TODO: plumbing from options.
	fsi, err := cgofuse.NewFuseInterface(goFS, dbgLog)
	if err != nil {
		panic(err)
	}

	closer, err := cgofuse.AttachToHost(fsi, fsid, target)
	if err != nil {
		panic(err)
	}

	return &fuseInterfaceFile{
		QID: p9.QID{Type: p9.TypeRegular},
		Attr: p9.Attr{
			Mode: p9.ModeRegular | attr.Mode.Permissions(),
			UID:  attr.UID,
			GID:  attr.GID,
		},
		mountpoint: closer,
	}
}

func (fsi *fuseInterfaceFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	log.Print("fuseInterfaceFile walk:", names)
	switch wnames := len(names); wnames {
	case 0:
		return nil, fsi, nil
	case 1:
		switch names[0] {
		case selfWName:
			return nil, fsi, nil
		}
	}
	return fsi.Walk(names)
}

func (fsi *fuseInterfaceFile) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	return p9.QID{}, 0, nil
}

func (fsi *fuseInterfaceFile) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	fsi.Attr.Apply(valid, attr)
	return nil
}

func (fsi *fuseInterfaceFile) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	var (
		qid          = fsi.QID
		filled, attr = fillAttrs(req, &fsi.Attr)
	)
	return qid, filled, *attr, nil
}
