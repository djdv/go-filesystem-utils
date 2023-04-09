package p9

import (
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

type (
	HostFile struct {
		directory
		makeGuestFn MakeGuestFunc
	}
	hosterSettings struct {
		directoryOptions []DirectoryOption
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
) (p9.QID, *HostFile, error) {
	var settings hosterSettings
	if err := parseOptions(&settings, options...); err != nil {
		return p9.QID{}, nil, err
	}
	qid, directory, err := NewDirectory(settings.directoryOptions...)
	if err != nil {
		return p9.QID{}, nil, err
	}
	return qid, &HostFile{
		directory:   directory,
		makeGuestFn: makeGuestFn,
	}, nil
}

func (hd *HostFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	qids, file, err := hd.directory.Walk(names)
	if len(names) == 0 {
		file = &HostFile{
			directory:   file,
			makeGuestFn: hd.makeGuestFn,
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
