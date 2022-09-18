package files

import (
	"errors"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

// TODO: docs; recommended / default value for this file's name
const MounterName = "mounts"

type mounter struct{ *Directory }

func NewMounter(options ...MetaOption) *mounter {
	var (
		_, dir  = NewDirectory(options...)
		mounter = &mounter{Directory: dir}
	)
	return mounter
}

func (dir *mounter) Mkdir(name string, permissions p9.FileMode, _ p9.UID, gid p9.GID) (p9.QID, error) {
	hostAPI, err := filesystem.ParseAPI(name)
	if err != nil {
		return p9.QID{}, err
	}
	if _, exists := dir.fileTable.load(name); exists {
		return p9.QID{}, perrors.EEXIST
	}
	dirOptions := []MetaOption{WithPath(dir.Directory.path)}
	switch hostAPI {
	case filesystem.Fuse:
		hostAPIDir := NewFuseDir(dirOptions...)
		if err := hostAPIDir.SetAttr(mkdirMask(permissions, dir.UID, gid)); err != nil {
			return *hostAPIDir.QID, err
		}

		return *hostAPIDir.QID, dir.Link(hostAPIDir, name)
	default:
		return p9.QID{}, errors.New("unexpected host") // TODO: msg
	}
}
