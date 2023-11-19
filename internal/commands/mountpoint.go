package commands

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/p9/p9"
)

type (
	mountPointHost[T any] interface {
		*T
		p9fs.Mounter
		p9fs.HostIdentifier
	}
	mountPointGuest[T any] interface {
		*T
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
	mountPointHosts  map[filesystem.Host]p9fs.MakeGuestFunc
	mountPointGuests map[filesystem.ID]p9fs.MakeMountPointFunc
)

func newMounter(parent p9.File, path ninePath,
	uid p9.UID, gid p9.GID, permissions p9.FileMode,
) (mountSubsystem, error) {
	const autoUnlink = true
	_, mountFS, err := p9fs.NewMounter(
		newMakeHostFunc(path, autoUnlink),
		p9fs.WithParent[p9fs.MounterOption](parent, mountsFileName),
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
		name:      mountsFileName,
		MountFile: mountFS,
	}, nil
}

func newMakeHostFunc(path ninePath, autoUnlink bool) p9fs.MakeHostFunc {
	hosts := makeMountPointHosts(path, autoUnlink)
	return func(parent p9.File, host filesystem.Host, mode p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, p9.File, error) {
		permissions, err := mountsDirCreatePreamble(mode)
		if err != nil {
			return p9.QID{}, nil, err
		}
		makeGuestFn, ok := hosts[host]
		if !ok {
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

func makeMountPointHosts(path ninePath, autoUnlink bool) mountPointHosts {
	type makeHostsFunc func(ninePath, bool) (filesystem.Host, p9fs.MakeGuestFunc)
	var (
		hostMakers = []makeHostsFunc{
			makeFUSEHost,
			makeNFSHost,
		}
		hosts = make(mountPointHosts, len(hostMakers))
	)
	for _, hostMaker := range hostMakers {
		host, guestMaker := hostMaker(path, autoUnlink)
		if guestMaker == nil {
			continue // System (likely) disabled by build constraints.
		}
		// No clobbering, accidental or otherwise.
		if _, exists := hosts[host]; exists {
			err := fmt.Errorf(
				"%s file constructor already registered",
				host,
			)
			panic(err)
		}
		hosts[host] = guestMaker
	}
	return hosts
}

func newMakeGuestFunc(guests mountPointGuests, path ninePath, autoUnlink bool) p9fs.MakeGuestFunc {
	return func(parent p9.File, guest filesystem.ID, mode p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, p9.File, error) {
		permissions, err := mountsDirCreatePreamble(mode)
		if err != nil {
			return p9.QID{}, nil, err
		}
		makeMountPointFn, ok := guests[guest]
		if !ok {
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

func makeMountPointGuests[
	T any,
	HC mountPointHost[T],
](path ninePath,
) mountPointGuests {
	guests := make(mountPointGuests)
	makeIPFSGuests[HC](guests, path)
	return guests
}

func mountsDirCreatePreamble(mode p9.FileMode) (p9.FileMode, error) {
	if !mode.IsDir() {
		return 0, generic.ConstError("expected to be called from mkdir")
	}
	return mode.Permissions(), nil
}

func newMountPointFunc[
	HC mountPointHost[HT],
	GT any,
	GC mountPointGuest[GT],
	HT any,
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
		prefix string
		parser p9fs.FieldParser
	)
	const (
		// TODO: [Go 1.21] use [errors.ErrUnsupported].
		unsupported    = generic.ConstError("unsupported operation")
		unsupportedFmt = "%w: %T does not implement field parser"
	)
	switch {
	case strings.HasPrefix(key, hostPrefix):
		prefix = hostPrefix
		var ok bool
		if parser, ok = any(&mp.Host).(p9fs.FieldParser); !ok {
			return fmt.Errorf(
				unsupportedFmt,
				unsupported, &mp.Host,
			)
		}
	case strings.HasPrefix(key, guestPrefix):
		prefix = guestPrefix
		var ok bool
		if parser, ok = any(&mp.Guest).(p9fs.FieldParser); !ok {
			return fmt.Errorf(
				unsupportedFmt,
				unsupported, &mp.Guest,
			)
		}
	default:
		const wildcard = "*"
		return p9fs.FieldError{
			Key:   key,
			Tried: []string{hostPrefix + wildcard, guestPrefix + wildcard},
		}
	}
	var (
		baseKey = key[len(prefix):]
		err     = parser.ParseField(baseKey, value)
	)
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
