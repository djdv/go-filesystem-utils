package commands

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/ipfs"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/p9/p9"
)

type (
	mountPointHost[T any] interface {
		*T
		p9fs.FieldParser
		p9fs.Mounter
		p9fs.HostIdentifier
	}
	mountPointGuest[T any] interface {
		*T
		p9fs.FieldParser
		p9fs.SystemMaker
		p9fs.GuestIdentifier
	}
	mountPoint[
		HT, GT any,
		HC mountPointHost[HT],
		GC mountPointGuest[GT],
	] struct {
		Host  HT `json:"host"`
		Guest GT `json:"guest"`
	}
)

func newGuestFunc[HC mountPointHost[T], T any](path ninePath, autoUnlink bool) p9fs.MakeGuestFunc {
	return func(parent p9.File, guest filesystem.ID, mode p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, p9.File, error) {
		permissions, err := mountsDirCreatePreamble(mode)
		if err != nil {
			return p9.QID{}, nil, err
		}
		var makeMountPointFn p9fs.MakeMountPointFunc
		// TODO: share IPFS instances
		// when server API is the same
		// (needs some wrapper too so
		// Close works properly.)
		switch guest {
		case ipfs.IPFSID:
			makeMountPointFn = newMountPointFunc[HC, *ipfs.IPFSGuest](path)
		case ipfs.PinFSID:
			makeMountPointFn = newMountPointFunc[HC, *ipfs.PinFSGuest](path)
		case ipfs.IPNSID:
			makeMountPointFn = newMountPointFunc[HC, *ipfs.IPNSGuest](path)
		case ipfs.KeyFSID:
			makeMountPointFn = newMountPointFunc[HC, *ipfs.KeyFSGuest](path)
		default:
			err := fmt.Errorf(`unexpected guest "%v"`, guest)
			return p9.QID{}, nil, err
		}
		return p9fs.NewGuestFile(
			makeMountPointFn,
			p9fs.UnlinkEmptyChildren[p9fs.GuestOption](autoUnlink),
			p9fs.UnlinkWhenEmpty[p9fs.GuestOption](autoUnlink),
			p9fs.WithParent[p9fs.GuestOption](parent, string(guest)),
			p9fs.WithPath[p9fs.GuestOption](path),
			p9fs.WithUID[p9fs.GuestOption](uid),
			p9fs.WithGID[p9fs.GuestOption](gid),
			p9fs.WithPermissions[p9fs.GuestOption](permissions),
		)
	}
}

func newMounter(parent p9.File, path ninePath,
	uid p9.UID, gid p9.GID, permissions p9.FileMode,
) (mountSubsystem, error) {
	const autoUnlink = true
	var (
		makeHostFn      = newHostFunc(path, autoUnlink)
		_, mountFS, err = p9fs.NewMounter(
			makeHostFn,
			p9fs.WithParent[p9fs.MounterOption](parent, mountsFileName),
			p9fs.WithPath[p9fs.MounterOption](path),
			p9fs.WithUID[p9fs.MounterOption](uid),
			p9fs.WithGID[p9fs.MounterOption](gid),
			p9fs.WithPermissions[p9fs.MounterOption](permissions),
			p9fs.UnlinkEmptyChildren[p9fs.MounterOption](autoUnlink),
			p9fs.WithoutRename[p9fs.MounterOption](true),
		)
	)
	if err != nil {
		return mountSubsystem{}, err
	}
	return mountSubsystem{
		name:      mountsFileName,
		MountFile: mountFS,
	}, nil
}

func newHostFunc(path ninePath, autoUnlink bool) p9fs.MakeHostFunc {
	return func(parent p9.File, host filesystem.Host, mode p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, p9.File, error) {
		permissions, err := mountsDirCreatePreamble(mode)
		if err != nil {
			return p9.QID{}, nil, err
		}
		var makeGuestFn p9fs.MakeGuestFunc
		switch host {
		case cgofuse.HostID:
			makeGuestFn = newGuestFunc[*cgofuse.Host](path, autoUnlink)
		default:
			err := fmt.Errorf(`unexpected host "%v"`, host)
			return p9.QID{}, nil, err
		}
		return p9fs.NewHostFile(
			makeGuestFn,
			p9fs.WithParent[p9fs.HosterOption](parent, string(host)),
			p9fs.WithPath[p9fs.HosterOption](path),
			p9fs.WithUID[p9fs.HosterOption](uid),
			p9fs.WithGID[p9fs.HosterOption](gid),
			p9fs.WithPermissions[p9fs.HosterOption](permissions),
			p9fs.UnlinkEmptyChildren[p9fs.HosterOption](autoUnlink),
			p9fs.UnlinkWhenEmpty[p9fs.HosterOption](autoUnlink),
			p9fs.WithoutRename[p9fs.HosterOption](true),
		)
	}
}

func mountsDirCreatePreamble(mode p9.FileMode) (p9.FileMode, error) {
	if !mode.IsDir() {
		return 0, generic.ConstError("expected to be called from mkdir")
	}
	return mode.Permissions(), nil
}

func newMountPointFunc[
	HC mountPointHost[HT],
	GC mountPointGuest[GT],
	HT, GT any,
](path ninePath,
) p9fs.MakeMountPointFunc {
	return func(parent p9.File, name string, mode p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, p9.File, error) {
		permissions, err := mountsFileCreatePreamble(mode)
		if err != nil {
			return p9.QID{}, nil, err
		}
		return p9fs.NewMountPoint[*mountPoint[HT, GT, HC, GC]](
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

func (mp *mountPoint[HT, GT, HC, GC]) ParseField(key, value string) error {
	const (
		hostPrefix  = "host."
		guestPrefix = "guest."
	)
	var (
		prefix  string
		parseFn func(_, _ string) error
	)
	switch {
	case strings.HasPrefix(key, hostPrefix):
		prefix = hostPrefix
		parseFn = HC(&mp.Host).ParseField
	case strings.HasPrefix(key, guestPrefix):
		prefix = guestPrefix
		parseFn = GC(&mp.Guest).ParseField
	default:
		const wildcard = "*"
		return p9fs.FieldError{
			Key:   key,
			Tried: []string{hostPrefix + wildcard, guestPrefix + wildcard},
		}
	}
	baseKey := key[len(prefix):]
	err := parseFn(baseKey, value)
	if err == nil {
		return nil
	}
	var fErr p9fs.FieldError
	if !errors.As(err, &fErr) {
		return err
	}
	tried := fErr.Tried
	for i, e := range fErr.Tried {
		tried[i] = prefix + e
	}
	fErr.Tried = tried
	return fErr
}

func (mp *mountPoint[HT, GT, HC, GC]) MakeFS() (fs.FS, error) {
	return GC(&mp.Guest).MakeFS()
}

func (mp *mountPoint[HT, GT, HC, GC]) Mount(fsys fs.FS) (io.Closer, error) {
	return HC(&mp.Host).Mount(fsys)
}

func (mp *mountPoint[HT, GT, HC, GC]) HostID() filesystem.Host {
	return HC(&mp.Host).HostID()
}

func (mp *mountPoint[HT, GT, HC, GC]) GuestID() filesystem.ID {
	return GC(&mp.Guest).GuestID()
}
