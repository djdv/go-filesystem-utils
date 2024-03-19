//go:build nofuse

package daemon

import (
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
)

const fuseHost = filesystem.Host("")

func makeFUSEHost(ninePath, bool) (filesystem.Host, p9fs.MakeGuestFunc) {
	return fuseHost, nil
}
