package files

import (
	"errors"
	"io"
	"io/fs"
	"sync/atomic"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

type (
	mountFunc = func(_ fs.FS, target string) (io.Closer, error) // TODO: placeholder name

	FSIDDir struct {
		file
		mount          mountFunc // TODO: placeholder name
		path           *atomic.Uint64
		cleanupEmpties bool
	}
)

func NewFSIDDir(fsid filesystem.ID, mountFn mountFunc, options ...FSIDOption) (p9.QID, *FSIDDir) {
	var settings fsidSettings
	if err := parseOptions(&settings, options...); err != nil {
		panic(err)
	}
	settings.RDev = p9.Dev(fsid)
	var (
		qid              p9.QID
		fsys             file
		unlinkSelf       = settings.cleanupSelf
		directoryOptions = []DirectoryOption{
			WithSuboptions[DirectoryOption](settings.metaSettings.asOptions()...),
			WithSuboptions[DirectoryOption](settings.linkSettings.asOptions()...),
		}
	)
	if unlinkSelf {
		qid, fsys = newEphemeralDirectory(directoryOptions...)
	} else {
		qid, fsys = NewDirectory(directoryOptions...)
	}
	return qid, &FSIDDir{
		path:           settings.path,
		file:           fsys,
		cleanupEmpties: settings.cleanupElements,
		mount:          mountFn,
	}
}

func (fsi *FSIDDir) fidOpened() bool { return false } // TODO need to store state or read &.dir's

func (fsi *FSIDDir) clone(withQID bool) ([]p9.QID, *FSIDDir, error) {
	var wnames []string
	if withQID {
		wnames = []string{selfWName}
	}
	var (
		qids, dirClone, err = fsi.file.Walk(wnames)
		newDir              = &FSIDDir{file: dirClone, path: fsi.path}
	)
	if err != nil {
		return nil, nil, err
	}
	return qids, newDir, nil
}

func (fsi *FSIDDir) Walk(names []string) ([]p9.QID, p9.File, error) {
	return walk[*FSIDDir](fsi, names...)
}

// TODO: stub out [Link] too?
func (dir *FSIDDir) Mkdir(name string, permissions p9.FileMode, _ p9.UID, gid p9.GID) (p9.QID, error) {
	return p9.QID{}, perrors.ENOSYS
}

func (mn *FSIDDir) Create(name string, flags p9.OpenFlags, permissions p9.FileMode,
	uid p9.UID, gid p9.GID,
) (p9.File, p9.QID, uint32, error) {
	if qid, err := mn.Mknod(name, permissions|p9.ModeRegular, 0, 0, uid, gid); err != nil {
		return nil, qid, 0, err
	}
	_, mf, err := mn.file.Walk([]string{name})
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
	major uint32, minor uint32, _ p9.UID, gid p9.GID,
) (p9.QID, error) {
	var (
		want      = p9.AttrMask{UID: true, RDev: true}
		required  = p9.AttrMask{RDev: true}
		attr, err = maybeGetAttrs(mn.file, want, required)
	)
	if err != nil {
		return p9.QID{}, err
	}
	// TODO: spec check; is mknod supposed to inherit permissions or only use the supplied?
	attr.Mode = mknodMask(mode)
	switch fsid := filesystem.ID(attr.RDev); fsid {
	case filesystem.IPFS, filesystem.IPFSPins,
		filesystem.IPNS, filesystem.IPFSKeys:
		var (
			metaOptions = []MetaOption{
				WithPath(mn.path),
				WithBaseAttr(attr),
				WithAttrTimestamps(true),
			}
			linkOptions = []LinkOption{
				WithParent(mn, name),
			}
			ipfsOptions = []IPFSOption{
				WithSuboptions[IPFSOption](metaOptions...),
				WithSuboptions[IPFSOption](linkOptions...),
			}
		)
		qid, ipfsFile, err := newIPFSMounter(fsid, mn.mount, ipfsOptions...)
		if err != nil {
			return p9.QID{}, err
		}
		return qid, mn.Link(ipfsFile, name)
	default:
		return p9.QID{}, errors.New("unexpected fsid") // TODO: real error
	}
}

func (mn *FSIDDir) UnlinkAt(name string, flags uint32) error {
	var (
		dir          = mn.file
		_, file, err = dir.Walk([]string{name})
	)
	if err != nil {
		return err
	}

	// TODO: we still need to {close | unlink} when encountering an error
	// after whichever side we decide to do first.

	if err := dir.UnlinkAt(name, flags); err != nil {
		return err
	}
	target, ok := file.(*ipfsMounter)
	if !ok {
		return perrors.EIO // TODO: better error?
	}
	if mountpoint := target.mountpoint; mountpoint != nil {
		return mountpoint.Close()
	}
	return nil
}
