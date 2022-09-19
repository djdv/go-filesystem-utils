package files

import (
	"errors"
	"log"
	"sync/atomic"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

type FSIDDir struct {
	p9.File
	path *atomic.Uint64
}

func NewFSIDDir(fsid filesystem.ID, options ...MetaOption) (p9.QID, *FSIDDir) {
	var (
		qid, dir = NewDirectory(options...)
		fsys     = &FSIDDir{File: dir, path: dir.path}
	)
	dir.Attr.RDev = p9.Dev(fsid)
	return qid, fsys
}

func (fsi *FSIDDir) fidOpened() bool { return false } // TODO need to store state or read &.dir's
func (fsi *FSIDDir) files() fileTable {
	// XXX: Magic; We need to change something to eliminate this.
	return fsi.File.(interface {
		files() fileTable
	}).files()
}

func (fsi *FSIDDir) clone(withQID bool) ([]p9.QID, *FSIDDir, error) {
	var wnames []string
	if withQID {
		wnames = []string{selfWName}
	}
	var (
		qids, dirClone, err = fsi.File.Walk(wnames)
		newDir              = &FSIDDir{File: dirClone, path: fsi.path}
	)
	if err != nil {
		return nil, nil, err
	}
	return qids, newDir, nil
}

func (fsi *FSIDDir) Walk(names []string) ([]p9.QID, p9.File, error) {
	return walk[*FSIDDir](fsi, names...)
}

func (mn *FSIDDir) Create(name string, flags p9.OpenFlags, permissions p9.FileMode,
	uid p9.UID, gid p9.GID,
) (p9.File, p9.QID, uint32, error) {
	if qid, err := mn.Mknod(name, permissions|p9.ModeRegular, 0, 0, uid, gid); err != nil {
		return nil, qid, 0, err
	}
	_, mf, err := mn.File.Walk([]string{name})
	if err != nil {
		return nil, p9.QID{}, 0, err
	}
	// TODO: clone here?
	// FIXME: Create makes+stores this file ptr, we flag it as opened
	// that is never cleared
	// walk should return a clone always? we should just unflag on close?
	qid, n, err := mf.Open(flags)
	if err != nil {
		return nil, p9.QID{}, 0, err
	}
	return mf, qid, n, nil
}

func (mn *FSIDDir) Mknod(name string, mode p9.FileMode,
	major uint32, minor uint32, uid p9.UID, gid p9.GID,
) (p9.QID, error) {
	_, _, attr, err := mn.File.GetAttr(p9.AttrMaskAll)
	if err != nil {
		return p9.QID{}, err
	}
	switch fsid := filesystem.ID(attr.RDev); fsid {
	case filesystem.IPFS, filesystem.IPFSPins,
		filesystem.IPNS, filesystem.IPFSKeys:
		mountFile := makeIPFSTarget(fsid)
		if err := setAttr(mountFile, &attr, true); err != nil {
			return p9.QID{}, err
		}
		log.Printf("linking %T \"%s\" to %T", mountFile, name, mn)
		return mountFile.QID, mn.Link(mountFile, name)
	default:
		return p9.QID{}, errors.New("unexpected fsid") // TODO: real error
	}
}

func (mn *FSIDDir) UnlinkAt(name string, flags uint32) error {
	log.Println("fsidir UnlinkAt:", name)
	_, tf, err := mn.File.Walk([]string{name})
	if err != nil {
		return err
	}

	// TODO: we still need to {close | unlink} when encountering an error
	// after whichever side we decide to do first.

	if err := mn.File.UnlinkAt(name, flags); err != nil {
		return err
	}
	target, ok := tf.(*ipfsTarget)
	if !ok {
		return perrors.EIO // TODO: better error?
	}
	if mountpoint := target.mountpoint; mountpoint != nil {
		return mountpoint.Close()
	}
	return nil
}

/*
func (mn *FSIDDir) UnlinkAt(name string, flags uint32) error {
	tf := mn.fileTable.pop(name)
	if tf == nil {
		return perrors.ENOENT
	}

	// TODO: better interface?
	target, ok := tf.(*ipfsTarget)
	if !ok {
		return perrors.EIO // TODO: better error?
	}
	if mountpoint := target.mountpoint; mountpoint != nil {
		return mountpoint.Close()
	}
	return nil
}
*/

/* old - touch implementation; no options possible
func (mn *FSIDDir) Create(name string, flags p9.OpenFlags, permissions p9.FileMode,
	uid p9.UID, gid p9.GID,
) (p9.File, p9.QID, uint32, error) {
	if qid, err := mn.Mknod(name, permissions|p9.ModeRegular, 0, 0, uid, gid); err != nil {
		return nil, qid, 0, err
	}
	_, mf, err := mn.Directory.Walk([]string{name})
	if err != nil {
		return nil, p9.QID{}, 0, err
	}
	qid, n, err := mf.Open(flags)
	if err != nil {
		return nil, p9.QID{}, 0, err
	}
	return mf, qid, n, nil
}

func (mn *FSIDDir) Mknod(name string, mode p9.FileMode,
	major uint32, minor uint32, uid p9.UID, gid p9.GID,
) (p9.QID, error) {
	parent := mn.Parent
	if parent == nil {
		return p9.QID{}, perrors.EIO // TODO: eVal
	}
	switch apiFile := parent.(type) {
	case *FuseDir:
		hostFile := apiFile.newHostFile(name, filesystem.ID(mn.Attr.RDev), mn.Attr)
		return hostFile.QID, mn.Link(hostFile, name)
	default:
		return p9.QID{}, perrors.EINVAL // TODO: eVal
	}
}
*/
