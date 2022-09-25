package files

import (
	"errors"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/hugelgupf/p9/p9"
)

// TODO: docs; recommended / default value for this file's name
const MounterName = "mounts"

type Mounter struct {
	file
	path ninePath
	linkSettings
	cleanupEmpties bool
}

func NewMounter(options ...MounterOption) *Mounter {
	var settings mounterSettings
	if err := parseOptions(&settings, options...); err != nil {
		panic(err)
	}
	var (
		fsys             file
		unlinkSelf       = settings.cleanupSelf
		directoryOptions = []DirectoryOption{
			WithSuboptions[DirectoryOption](settings.metaSettings.asOptions()...),
			WithSuboptions[DirectoryOption](settings.linkSettings.asOptions()...),
		}
	)
	if unlinkSelf {
		_, fsys = newEphemeralDirectory(directoryOptions...)
	} else {
		_, fsys = NewDirectory(directoryOptions...)
	}
	return &Mounter{
		path:           settings.path,
		file:           fsys,
		cleanupEmpties: settings.cleanupElements,
	}
}

func (dir *Mounter) Mkdir(name string, permissions p9.FileMode, _ p9.UID, gid p9.GID) (p9.QID, error) {
	hostAPI, err := filesystem.ParseAPI(name)
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
	switch hostAPI {
	/*
		case filesystem.Plan9Protocol:
			// FIXME: implement
			return p9.QID{}, errors.New("not fully implemented yet")
			qid, nineDir := NewNineDir(WithSuboptions[NineOption](directoryOptions...))
			return qid, dir.Link(nineDir, name)
	*/
	case filesystem.Fuse:
		qid, fuseDir := NewFuseDir(
			WithSuboptions[FuseOption](metaOptions...),
			WithSuboptions[FuseOption](linkOptions...),
			WithSuboptions[FuseOption](generatorOptions...),
		)
		return qid, dir.Link(fuseDir, name)
	default:
		return p9.QID{}, errors.New("unexpected host") // TODO: msg
	}
}
