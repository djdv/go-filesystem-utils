package commands

import (
	"flag"
	"fmt"
	"strconv"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
	nfs "github.com/djdv/go-filesystem-utils/internal/filesystem/nfs/client"
	"github.com/multiformats/go-multiaddr"
)

type nfsGuest nfs.FSMaker

const (
	nfsClientFlagPrefix     = "nfs-client-"
	nfsClientServerFlagName = nfsClientFlagPrefix + "server"
)

func makeNFSGuestCommand[
	hI hostPtr[hT],
	hT any,
](filesystem.Host,
) command.Command {
	return newMountCommand[hI, *nfsGuest]()
}

func (*nfsGuest) ID() filesystem.ID { return nfs.ID }

func (*nfsGuest) usage(filesystem.Host) string {
	return string(nfs.ID) + " attaches to an NFS file server"
}

func (settings *nfsGuest) BindFlags(flagSet *flag.FlagSet) {
	*settings = nfsGuest{
		Dirpath: nfs.DefaultDirpath,
	}
	settings.bindMaddrFlag(flagSet)
	settings.bindHostnameFlag(flagSet)
	settings.bindDirpathFlag(flagSet)
	settings.bindLinkSeparatorFlag(flagSet)
	settings.bindLinkLimit(flagSet)
	settings.bindUIDFlag(flagSet)
	settings.bindGIDFlag(flagSet)
}

func (settings *nfsGuest) newSetFunc(attribute string) flagSetFunc {
	return func(argument string) error {
		return (*nfs.FSMaker)(settings).ParseField(attribute, argument)
	}
}

func (settings *nfsGuest) bindMaddrFlag(flagSet *flag.FlagSet) {
	const (
		name  = nfsClientServerFlagName
		usage = "NFS server `maddr`"
	)
	setFlagOnce[multiaddr.Multiaddr](
		flagSet, name, usage,
		settings.newSetFunc(nfs.MaddrAttribute),
	)
}

func (settings *nfsGuest) bindHostnameFlag(flagSet *flag.FlagSet) {
	const (
		name  = nfsClientFlagPrefix + "hostname"
		usage = "client's `hostname` used for `AUTH_UNIX`"
	)
	setFlagOnce[string](
		flagSet, name, usage,
		settings.newSetFunc(nfs.HostnameAttribute),
	)
	flagSet.Lookup(name).
		DefValue = "caller's hostname"
}

func (settings *nfsGuest) bindDirpathFlag(flagSet *flag.FlagSet) {
	const (
		name  = nfsClientFlagPrefix + "dirpath"
		usage = "`dirpath` on the server, to be mounted"
	)
	setFlagOnce[string](
		flagSet, name, usage,
		settings.newSetFunc(nfs.DirpathAttribute),
	)
	flagSet.Lookup(name).
		DefValue = nfs.DefaultDirpath
}

func (settings *nfsGuest) bindLinkSeparatorFlag(flagSet *flag.FlagSet) {
	const (
		name  = nfsClientFlagPrefix + "link-separator"
		usage = "sets a `separator` to use when normalizing" +
			" symbolic link targets, during internal file system operations"
	)
	setFlagOnce[string](
		flagSet, name, usage,
		settings.newSetFunc(nfs.LinkSeparatorAttribute),
	)
	flagSet.Lookup(name).
		DefValue = nfs.DefaultLinkSeparator
}

func (settings *nfsGuest) bindLinkLimit(flagSet *flag.FlagSet) {
	const (
		name  = nfsClientFlagPrefix + "link-limit"
		usage = "sets the maximum amount of times an" +
			" operation will resolve a symbolic link chain," +
			" before it returns a recursion error"
	)
	setFlagOnce[uint](
		flagSet, name, usage,
		settings.newSetFunc(nfs.LinkLimitAttribute),
	)
	flagSet.Lookup(name).
		DefValue = strconv.Itoa(nfs.DefaultLinkLimit)
}

func (settings *nfsGuest) bindUIDFlag(flagSet *flag.FlagSet) {
	const (
		name  = nfsClientFlagPrefix + "uid"
		usage = "client's `uid` used for `AUTH_UNIX`"
	)
	setFlagOnce[uint32](
		flagSet, name, usage,
		settings.newSetFunc(nfs.UIDAttribute),
	)
	flagSet.Lookup(name).
		DefValue = "caller's uid"
}

func (settings *nfsGuest) bindGIDFlag(flagSet *flag.FlagSet) {
	const (
		name  = nfsClientFlagPrefix + "gid"
		usage = "client's `gid` used for `AUTH_UNIX`"
	)
	setFlagOnce[uint32](
		flagSet, name, usage,
		settings.newSetFunc(nfs.GIDAttribute),
	)
	flagSet.Lookup(name).
		DefValue = "caller's gid"
}

func (settings *nfsGuest) make() (mountpoint.Guest, error) {
	if settings.Maddr.Multiaddr == nil {
		const name = nfsClientServerFlagName
		return nil, fmt.Errorf(
			"flag `-%s` must be provided for NFS clients",
			name,
		)
	}
	return (*nfs.FSMaker)(settings), nil
}
