package files

import (
	"sync/atomic"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
	"github.com/hugelgupf/p9/p9"
)

type (
	FuseDir struct {
		file
		path           *atomic.Uint64
		cleanupEmpties bool
	}
)

func NewFuseDir(options ...FuseOption) (p9.QID, *FuseDir) {
	var settings fuseSettings
	if err := parseOptions(&settings, options...); err != nil {
		panic(err)
	}
	settings.RDev = p9.Dev(filesystem.Fuse)
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
	return qid, &FuseDir{
		path:           settings.path,
		file:           fsys,
		cleanupEmpties: settings.cleanupElements,
	}
}

func (dir *FuseDir) Mkdir(name string, permissions p9.FileMode, _ p9.UID, gid p9.GID) (p9.QID, error) {
	fsid, err := filesystem.ParseID(name)
	if err != nil {
		return p9.QID{}, err
	}
	attr, err := mkdirInherit(dir, permissions, gid)
	if err != nil {
		return p9.QID{}, err
	}
	var (
		metaOptions = []MetaOption{
			WithPath(dir.path),
			WithBaseAttr(attr),
			WithAttrTimestamps(true),
		}
		linkOptions = []LinkOption{
			WithParent(dir, name),
		}
		generatorOptions []GeneratorOption
	)
	if dir.cleanupEmpties {
		generatorOptions = append(generatorOptions,
			CleanupSelf(true),
			CleanupEmpties(true),
		)
	}
	qid, fsiDir := NewFSIDDir(fsid, cgofuse.MountFuse,
		WithSuboptions[FSIDOption](metaOptions...),
		WithSuboptions[FSIDOption](linkOptions...),
		WithSuboptions[FSIDOption](generatorOptions...),
	)
	return qid, dir.Link(fsiDir, name)
}
