//go:build !nonfs

package daemon

import (
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
)

func makeNFSHost(path ninePath, autoUnlink bool) (filesystem.Host, p9fs.MakeGuestFunc) {
	panic("NIY")
	/*
		guests := makeMountPointGuests[nfs.Host](path)
		return nfs.HostID, newMakeGuestFunc(guests, path, autoUnlink)
	*/
}
