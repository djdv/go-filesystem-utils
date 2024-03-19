package daemon

import (
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/p9/p9"
)

func newMounter(parent p9.File, path ninePath,
	uid p9.UID, gid p9.GID, permissions p9.FileMode,
) (mountSubsystem, error) {
	const autoUnlink = true
	_, mountFS, err := p9fs.NewMounter(
		newMakeHostFunc(path, autoUnlink),
		p9fs.WithParent[p9fs.MounterOption](parent, MountsFileName),
		p9fs.WithPath[p9fs.MounterOption](path),
		p9fs.WithUID[p9fs.MounterOption](uid),
		p9fs.WithGID[p9fs.MounterOption](gid),
		p9fs.WithPermissions[p9fs.MounterOption](permissions),
		p9fs.UnlinkEmptyChildren[p9fs.MounterOption](autoUnlink),
		p9fs.WithoutRename[p9fs.MounterOption](true),
	)
	if err != nil {
		return mountSubsystem{}, err
	}
	return mountSubsystem{
		name:      MountsFileName,
		MountFile: mountFS,
	}, nil
}

func mountsDirCreatePreamble(mode p9.FileMode) (p9.FileMode, error) {
	if !mode.IsDir() {
		return 0, generic.ConstError("expected to be called from mkdir")
	}
	return mode.Permissions(), nil
}

func newMountPointFunc[
	hostI hostPtr[hostT],
	guestT any,
	guestI guestPtr[guestT],
	hostT any,
](hostID filesystem.Host, guestID filesystem.ID, path ninePath,
) p9fs.MakeMountPointFunc {
	return func(parent p9.File, name string, mode p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, p9.File, error) {
		permissions, err := mountsFileCreatePreamble(mode)
		if err != nil {
			return p9.QID{}, nil, err
		}
		var (
			host  hostI  = new(hostT)
			guest guestI = new(guestT)
			pair         = mountpoint.NewPair(
				hostID, guestID,
				host, guest,
			)
		)
		return p9fs.NewMountpointFile(
			pair,
			p9fs.WithParent[p9fs.MountPointOption](parent, name),
			p9fs.WithPath[p9fs.MountPointOption](path),
			p9fs.WithUID[p9fs.MountPointOption](uid),
			p9fs.WithGID[p9fs.MountPointOption](gid),
			p9fs.WithPermissions[p9fs.MountPointOption](permissions),
		)
	}
}

func mountsFileCreatePreamble(mode p9.FileMode) (p9.FileMode, error) {
	if !mode.IsRegular() {
		return 0, generic.ConstError("expected to be called from mknod")
	}
	return mode.Permissions(), nil
}
