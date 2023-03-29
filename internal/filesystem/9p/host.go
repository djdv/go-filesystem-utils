package p9

import (
	"sync/atomic"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

type (
	HostFile struct {
		directory
		path           *atomic.Uint64
		makeGuestFn    MakeGuestFunc
		cleanupEmpties bool
	}
	hosterSettings struct {
		directorySettings
		generatorSettings
	}
	HosterOption func(*hosterSettings) error
	// MakeGuestFunc should handle file creation operations
	// for files representing a [filesystem.ID].
	MakeGuestFunc func(parent p9.File, guest filesystem.ID,
		permissions p9.FileMode,
		uid p9.UID, gid p9.GID) (p9.QID, p9.File, error)
)

func NewHostFile(makeGuestFn MakeGuestFunc,
	options ...HosterOption,
) (p9.QID, *HostFile) {
	settings := hosterSettings{
		directorySettings: directorySettings{
			metadata: makeMetadata(p9.ModeDirectory),
		},
	}
	if err := parseOptions(&settings, options...); err != nil {
		panic(err)
	}
	var (
		unlinkSelf = settings.cleanupSelf
		dirOpts    = settings.directorySettings.asOptions()
		qid, fsys  = newDirectory(unlinkSelf, dirOpts...)
	)
	return qid, &HostFile{
		path:           settings.ninePath,
		directory:      fsys,
		cleanupEmpties: settings.cleanupElements,
		makeGuestFn:    makeGuestFn,
	}
}

func (hd *HostFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	qids, file, err := hd.directory.Walk(names)
	if len(names) == 0 {
		file = &HostFile{
			directory:      file,
			path:           hd.path,
			cleanupEmpties: hd.cleanupEmpties,
			makeGuestFn:    hd.makeGuestFn,
		}
	}
	return qids, file, err
}

func (hd *HostFile) Mkdir(name string, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, error) {
	uid, gid, err := mkPreamble(hd, name, uid, gid)
	if err != nil {
		return p9.QID{}, err
	}
	qid, file, err := hd.makeGuestFn(hd, filesystem.ID(name),
		permissions, uid, gid)
	if err != nil {
		return p9.QID{}, fserrors.Join(perrors.EACCES, err)
	}
	return qid, hd.Link(file, name)
}
