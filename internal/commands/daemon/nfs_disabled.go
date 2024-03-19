//go:build nonfs

package daemon

import (
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
)

const nfsHost = filesystem.Host("")

func makeNFSHost(ninePath, bool) (filesystem.Host, p9fs.MakeGuestFunc) {
	return nfsHost, nil
}
