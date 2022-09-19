package files

import (
	"io"
	"io/fs"
	"log"
	"sync/atomic"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
)

type (
	FuseDir struct {
		p9.File
		path *atomic.Uint64
	}
	fuseInterfaceFile struct {
		templatefs.NoopFile
		mountpoint io.Closer
		p9.Attr
		p9.QID
	}
)

func NewFuseDir(options ...MetaOption) (p9.QID, *FuseDir) {
	var (
		qid, dir = NewDirectory(options...)
		fsys     = &FuseDir{File: dir, path: dir.path}
	)
	dir.Attr.RDev = p9.Dev(filesystem.Fuse)
	return qid, fsys
}

func (dir *FuseDir) fidOpened() bool { return false } // TODO need to store state or read &.dir's
func (dir *FuseDir) files() fileTable {
	// XXX: Magic; We need to change something to eliminate this.
	return dir.File.(interface {
		files() fileTable
	}).files()
}

func (dir *FuseDir) clone(withQID bool) ([]p9.QID, *FuseDir, error) {
	var wnames []string
	if withQID {
		wnames = []string{selfWName}
	}
	var (
		qids, dirClone, err = dir.File.Walk(wnames)
		newDir              = &FuseDir{File: dirClone, path: dir.path}
	)
	if err != nil {
		return nil, nil, err
	}
	return qids, newDir, nil
}

func (dir *FuseDir) Walk(names []string) ([]p9.QID, p9.File, error) {
	return walk[*FuseDir](dir, names...)
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

	log.Printf("linking %T \"%s\" to %T", eDir, name, dir)
	return qid, dir.Link(eDir, name)
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

/* TODO: if we're empty, close the fuse/WinFSP interface?
func (mn *FuseDir) UnlinkAt(name string, flags uint32) error {
	log.Printf("fusedir UnlinkAt: %s from %T", name, mn.File)
	return mn.File.UnlinkAt(name, flags)
}
*/
