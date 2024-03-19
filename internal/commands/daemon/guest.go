package daemon

import (
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
	"github.com/djdv/p9/p9"
)

type (
	mountPointGuests     map[filesystem.ID]p9fs.MakeMountPointFunc
	guestPtr[guestT any] interface {
		*guestT
		mountpoint.Guest
	}
)

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
	host any,
	hostI hostPtr[host],
](hostID filesystem.Host, path ninePath,
) mountPointGuests {
	guests := make(mountPointGuests)
	makeIPFSGuests[hostI](hostID, guests, path)
	// makeNFSGuest[HC](guests, path)
	return guests
}
