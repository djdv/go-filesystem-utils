//go:build !nofuse

package daemon

import (
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
)

func makeFUSEHost(path ninePath, autoUnlink bool) (filesystem.Host, p9fs.MakeGuestFunc) {
	guests := makeMountPointGuests[cgofuse.Mounter](cgofuse.Host, path)
	return cgofuse.Host, newMakeGuestFunc(guests, path, autoUnlink)
}
