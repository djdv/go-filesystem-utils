package p9

import (
	"errors"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/hugelgupf/p9/p9"
)

// TODO: docs; recommended / default value for this file's name
const MounterName = "mounts"

type Mounter struct {
	directory
	cleanupEmpties bool
}

// TODO: [current] - define a type map[host]handlerFunc() that we take in
// may need map[fsid] handler too.
// From main -> us -> main
// internally we'll use this table dynamically, rather than the hardcoded consts we had before
// these func may be defined by main, and/or in some relevant package
// e.g. `cgofuse.MountFunc`
func NewMounter(options ...MounterOption) *Mounter {
	var settings mounterSettings
	if err := parseOptions(&settings, options...); err != nil {
		panic(err)
	}
	var (
		fsys             directory
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
		directory:      fsys,
		cleanupEmpties: settings.cleanupElements,
	}
}

func (mn *Mounter) clone(withQID bool) ([]p9.QID, *Mounter, error) {
	var wnames []string
	if withQID {
		wnames = []string{selfWName}
	}
	qids, dirClone, err := mn.directory.Walk(wnames)
	if err != nil {
		return nil, nil, err
	}
	typedDir, err := assertDirectory(dirClone)
	if err != nil {
		return nil, nil, err
	}
	newDir := &Mounter{
		directory: typedDir,

		cleanupEmpties: mn.cleanupEmpties,
	}
	if err != nil {
		return nil, nil, err
	}
	return qids, newDir, nil
}

/*
func (mn *Mounter) Walk(names []string) ([]p9.QID, p9.File, error) {
	return walk[*Mounter](mn, names...)
}

func (mn *Mounter) Open(mode p9.OpenFlags) (p9.QID, ioUnit, error) {
}
*/

func (mn *Mounter) Mkdir(name string, permissions p9.FileMode, _ p9.UID, gid p9.GID) (p9.QID, error) {
	hostAPI, err := filesystem.ParseAPI(name)
	if err != nil {
		return p9.QID{}, err
	}
	attr, err := mkdirInherit(mn, permissions, gid)
	if err != nil {
		return p9.QID{}, err
	}
	var (
		metaOptions = []MetaOption{
			WithPath(mn.directory.path()),
			WithBaseAttr(attr),
			WithAttrTimestamps(true),
		}
		linkOptions = []LinkOption{
			WithParent(mn, name),
		}
		generatorOptions []GeneratorOption
	)
	if mn.cleanupEmpties {
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
		return qid, mn.Link(fuseDir, name)
	default:
		return p9.QID{}, errors.New("unexpected host") // TODO: msg
	}
}
