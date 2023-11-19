//go:build !nonfs

package commands

import (
	"encoding/json"
	"errors"
	"flag"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/nfs"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/multiformats/go-multiaddr"
)

type (
	nfsHostSettings nfs.Host
	nfsHostOption   func(*nfsHostSettings) error
	nfsHostOptions  []nfsHostOption
)

func makeNFSCommand() command.Command {
	return makeMountSubcommand(
		nfs.HostID,
		makeGuestCommands[nfsHostOptions, nfsHostSettings](nfs.HostID),
	)
}

func makeNFSHost(path ninePath, autoUnlink bool) (filesystem.Host, p9fs.MakeGuestFunc) {
	guests := makeMountPointGuests[nfs.Host](path)
	return nfs.HostID, newMakeGuestFunc(guests, path, autoUnlink)
}

func (on *nfsHostOptions) BindFlags(flagSet *flag.FlagSet) { /* NOOP */ }

func (on nfsHostOptions) make() (nfsHostSettings, error) {
	return makeWithOptions(on...)
}

func (*nfsHostOptions) usage(guest filesystem.ID) string {
	return string(nfs.HostID) + " hosts " +
		string(guest) + " as an NFS server"
}

func (set nfsHostSettings) marshal(arg string) ([]byte, error) {
	if arg == "" {
		err := command.UsageError{
			Err: generic.ConstError(
				"expected server multiaddr",
			),
		}
		return nil, err
	}
	maddr, err := multiaddr.NewMultiaddr(arg)
	if err != nil {
		return nil, err
	}
	set.Maddr = maddr
	return json.Marshal(set)
}

func unmarshalNFS() (filesystem.Host, decodeFunc) {
	return nfs.HostID, func(b []byte) (string, error) {
		var host nfs.Host
		if err := json.Unmarshal(b, &host); err != nil {
			return "", err
		}
		if maddr := host.Maddr; maddr != nil {
			return host.Maddr.String(), nil
		}
		return "", errors.New("NFS host address was not present in the mountpoint data")
	}
}
