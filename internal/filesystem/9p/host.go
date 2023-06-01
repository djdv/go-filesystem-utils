package p9

import (
	"errors"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	perrors "github.com/djdv/p9/errors"
	"github.com/djdv/p9/p9"
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
	// The file `mode` will contain file type bits.
	MakeGuestFunc func(parent p9.File, guest filesystem.ID,
		mode p9.FileMode,
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
	mode := permissions | p9.ModeDirectory
	qid, file, err := hd.makeGuestFn(hd, filesystem.ID(name),
		mode, uid, gid)
	if err != nil {
		return p9.QID{}, errors.Join(perrors.EACCES, err)
	}
	return qid, hd.Link(file, name)
}

func (hf *HostFile) Create(name string, flags p9.OpenFlags, permissions p9.FileMode,
	uid p9.UID, gid p9.GID,
) (p9.File, p9.QID, uint32, error) {
	return createViaMknod(hf, name, flags, permissions, uid, gid)
}

func (hf *HostFile) Mknod(name string, mode p9.FileMode,
	_, _ uint32, uid p9.UID, gid p9.GID,
) (p9.QID, error) {
	uid, gid, err := mkPreamble(hf, name, uid, gid)
	if err != nil {
		return p9.QID{}, err
	}
	mode |= p9.ModeRegular
	qid, file, err := hf.makeGuestFn(hf, filesystem.ID(name),
		mode, uid, gid)
	if err != nil {
		return p9.QID{}, errors.Join(perrors.EACCES, err)
	}
	return qid, hf.Link(file, name)
}
