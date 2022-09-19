package files

import (
	"errors"
	"sync/atomic"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/hugelgupf/p9/p9"
)

// TODO: docs; recommended / default value for this file's name
const MounterName = "mounts"

type mounter struct {
	p9.File
	path *atomic.Uint64
}

func NewMounter(options ...MetaOption) *mounter {
	var (
		_, root = NewDirectory(options...)
		mounter = &mounter{File: root, path: root.path}
	)
	return mounter
}

func (dir *mounter) Mkdir(name string, permissions p9.FileMode, _ p9.UID, gid p9.GID) (p9.QID, error) {
	hostAPI, err := filesystem.ParseAPI(name)
	if err != nil {
		return p9.QID{}, err
	}
	want := p9.AttrMask{UID: true}
	_, valid, attr, err := dir.GetAttr(want)
	if err != nil {
		return p9.QID{}, err
	}
	if !valid.Contains(want) {
		return p9.QID{}, attrErr(valid, want)
	}
	const withServerTimes = true
	switch hostAPI {
	case filesystem.Fuse:
		var ( // TODO: Proper.
			qid, eDir  = newEphemeralDir(dir, name, WithPath(dir.path))
			hostAPIDir = &FuseDir{
				File: eDir,
				path: dir.path,
			}
		) //
		// TODO: need to be able to set this like constructor does.
		// eDir.Attr.RDev = p9.Dev(filesystem.Fuse)
		// HACK:
		eDir.File.(*Directory).RDev = p9.Dev(filesystem.Fuse)
		if err := setAttr(hostAPIDir, &p9.Attr{
			Mode: (permissions.Permissions() &^ S_LINMSK) & S_IRWXA,
			UID:  attr.UID,
			GID:  gid,
		}, withServerTimes); err != nil {
			return qid, err
		}
		return qid, dir.Link(hostAPIDir, name)
	default:
		return p9.QID{}, errors.New("unexpected host") // TODO: msg
	}
}
