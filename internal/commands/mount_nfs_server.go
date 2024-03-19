//go:build !nonfs

package commands

import (
	"flag"
	"strconv"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
	nfsc "github.com/djdv/go-filesystem-utils/internal/filesystem/nfs/client"
	nfs "github.com/djdv/go-filesystem-utils/internal/filesystem/nfs/server"
	"github.com/multiformats/go-multiaddr"
)

type nfsHost nfs.Mounter

func makeNFSHostCommand() command.Command {
	return newMountSubcommand(
		nfs.ID,
		makeGuestCommands[*nfsHost](nfs.ID),
	)
}

func (*nfsHost) ID() filesystem.Host {
	return nfs.ID
}

func (*nfsHost) usage(guest filesystem.ID) string {
	guestStr := string(guest)
	if guest == nfsc.ID {
		guestStr += "(client)"
	}
	return string(nfs.ID) + " hosts " + guestStr + " as an NFS server"
}

func (settings *nfsHost) BindFlags(flagSet *flag.FlagSet) {
	*settings = nfsHost{
		CacheLimit: nfs.DefaultCacheLimit,
	}
	settings.bindCacheLimitFlag(flagSet)
}

func (settings *nfsHost) newSetFunc(attribute string) flagSetFunc {
	return func(argument string) error {
		return (*nfs.Mounter)(settings).ParseField(attribute, argument)
	}
}

func (settings *nfsHost) bindCacheLimitFlag(flagSet *flag.FlagSet) {
	const (
		prefix = "nfs-server-"
		name   = prefix + "cache-limit"
		usage  = "sets how many handles and entries the server will cache"
	)
	setFlagOnce[int](
		flagSet, name, usage,
		settings.newSetFunc(nfs.MaddrAttribute),
	)
	flagSet.Lookup(name).
		DefValue = strconv.Itoa(settings.CacheLimit)
}

func (settings *nfsHost) make(point string) (mountpoint.Host, error) {
	maddr, err := multiaddr.NewMultiaddr(point)
	if err != nil {
		return nil, err
	}
	clone := (nfs.Mounter)(*settings)
	clone.Maddr.Multiaddr = maddr
	return &clone, nil
}
