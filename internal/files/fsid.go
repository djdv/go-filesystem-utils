package files

import (
	"errors"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

type FSIDDir struct{ *Directory }

func NewFSIDDir(fsid filesystem.ID, options ...MetaOption) *FSIDDir {
	var (
		_, dir = NewDirectory(options...)
		fsys   = &FSIDDir{Directory: dir}
	)
	fsys.Attr.RDev = p9.Dev(fsid)
	return fsys
}

func (fsi *FSIDDir) clone(withQID bool) ([]p9.QID, *FSIDDir, error) {
	qids, dirClone, err := fsi.Directory.clone(withQID)
	return qids, &FSIDDir{Directory: dirClone}, err
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
	_, mf, err := mn.Directory.Walk([]string{name})
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
	switch fsid := filesystem.ID(mn.RDev); fsid {
	case filesystem.IPFS, filesystem.IPFSPins,
		filesystem.IPNS, filesystem.IPFSKeys:
		mountFile := makeIPFSTarget(fsid)
		if err := mountFile.SetAttr(attrToSetAttr(mn.Attr)); err != nil {
			return mountFile.QID, err
		}
		return mountFile.QID, mn.Link(mountFile, name)
	default:
		return p9.QID{}, errors.New("unexpected fsid") // TODO: real error
	}
}

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
