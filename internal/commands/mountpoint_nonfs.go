//go:build nonfs

package commands

import (
	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
)

const nfsHost = filesystem.Host("")

func makeNFSCommand() command.Command {
	return nil
}

func makeNFSHost(ninePath, bool) (filesystem.Host, p9fs.MakeGuestFunc) {
	return nfsHost, nil
}

func unmarshalNFS() (filesystem.Host, decodeFunc) {
	return nfsHost, nil
}

func makeNFSGuestCommand[
	HC mountCmdHost[HT, HM],
	HM marshaller,
	HT any,
](host filesystem.Host,
) command.Command {
	return nil
}

func makeNFSGuest[
	HC mountPointHost[T],
	T any,
](mountPointGuests, ninePath,
) { /*NOOP*/ }
