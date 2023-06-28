//go:build nofuse

package commands

import (
	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
)

type fuseID uint32

const fuseHost = filesystem.Host("")

func makeFUSECommand() command.Command {
	return nil
}

func makeFUSEHost(ninePath, bool) (filesystem.Host, p9fs.MakeGuestFunc) {
	return fuseHost, nil
}

func unmarshalFUSE() (filesystem.Host, decodeFunc) {
	return fuseHost, nil
}
