package daemon

import (
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
	"github.com/djdv/p9/p9"
)

type (
	mountPointHosts    map[filesystem.Host]p9fs.MakeGuestFunc
	hostPtr[hostT any] interface {
		*hostT
		mountpoint.Host
	}
)

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
			// makeNFSHost,
		}
		hosts = make(mountPointHosts, len(hostMakers))
	)
	for _, hostMaker := range hostMakers {
		host, guestMaker := hostMaker(path, autoUnlink)
		if guestMaker == nil {
			continue // System (likely) disabled by build constraints.
		}
		hosts[host] = guestMaker
	}
	return hosts
}
