package p9

import (
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

type (
	GuestFile struct {
		directory
		makeMountPointFn MakeMountPointFunc
	}
	guestSettings struct {
		directoryOptions []DirectoryOption
	}
	GuestOption func(*guestSettings) error
	// MakeMountPointFunc should handle file creation operations
	// for files representing mount points.
	// The file `mode` will contain file type bits.
	MakeMountPointFunc func(parent p9.File, name string,
		mode p9.FileMode, uid p9.UID, gid p9.GID,
	) (p9.QID, p9.File, error)
	detacher interface {
		detach() error
	}
)

func NewGuestFile(makeMountPointFn MakeMountPointFunc, options ...GuestOption,
) (p9.QID, *GuestFile, error) {
	var settings guestSettings
	if err := parseOptions(&settings, options...); err != nil {
		return p9.QID{}, nil, err
	}
	qid, directory, err := NewDirectory(settings.directoryOptions...)
	if err != nil {
		return p9.QID{}, nil, err
	}
	return qid, &GuestFile{
		directory:        directory,
		makeMountPointFn: makeMountPointFn,
	}, nil
}

func (gf *GuestFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	qids, file, err := gf.directory.Walk(names)
	if len(names) == 0 {
		file = &GuestFile{
			directory:        file,
			makeMountPointFn: gf.makeMountPointFn,
		}
	}
	return qids, file, err
}

// TODO: stub out [Link] too?
func (gf *GuestFile) Mkdir(name string, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, error) {
	uid, gid, err := mkPreamble(gf, name, uid, gid)
	if err != nil {
		return p9.QID{}, err
	}
	mode := permissions | p9.ModeDirectory
	qid, file, err := gf.makeMountPointFn(gf, name,
		mode, uid, gid)
	if err != nil {
		return p9.QID{}, fserrors.Join(perrors.EACCES, err)
	}
	return qid, gf.Link(file, name)
}

func (gf *GuestFile) Create(name string, flags p9.OpenFlags, permissions p9.FileMode,
	uid p9.UID, gid p9.GID,
) (p9.File, p9.QID, uint32, error) {
	return createViaMknod(gf, name, flags, permissions, uid, gid)
}

func (gf *GuestFile) Mknod(name string, mode p9.FileMode,
	_, _ uint32, uid p9.UID, gid p9.GID,
) (p9.QID, error) {
	uid, gid, err := mkPreamble(gf, name, uid, gid)
	if err != nil {
		return p9.QID{}, err
	}
	mode |= p9.ModeRegular
	qid, file, err := gf.makeMountPointFn(gf, name,
		mode, uid, gid)
	if err != nil {
		return p9.QID{}, err
	}
	return qid, gf.Link(file, name)
}

func (gf *GuestFile) UnlinkAt(name string, flags uint32) error {
	var (
		dir          = gf.directory
		_, file, err = dir.Walk([]string{name})
	)
	if err != nil {
		return err
	}
	// NOTE: Always attempt both operations,
	// regardless of error from preceding operation.
	var dErr error
	if target, ok := file.(detacher); ok {
		dErr = target.detach()
	}
	uErr := dir.UnlinkAt(name, flags)
	return fserrors.Join(dErr, uErr)
}
