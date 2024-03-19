//go:build !noipfs

package commands

import (
	"flag"
	"strconv"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/ipfs"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
)

// ipfsSettings adapts [ipfs.FSMaker]
// for use with the CLI. Particularly
// related to [flag] and [command.Command].
type ipfsSettings ipfs.FSMaker

const ipfsID = ipfs.ID

func (*ipfsSettings) ID() filesystem.ID { return ipfsID }

func (*ipfsSettings) usage(filesystem.Host) string {
	return string(ipfsID) + " provides an empty root directory." +
		"\nChild paths are forwarded to the IPFS API."
}

func (settings *ipfsSettings) BindFlags(flagSet *flag.FlagSet) {
	const (
		system = ipfsID
		prefix = "ipfs-"
	)
	*settings = ipfsSettings{
		APITimeout:          ipfs.DefaultAPITimeout,
		Permissions:         ipfs.DefaultPermissions,
		NodeCacheCount:      ipfs.DefaultNodeCacheCount,
		DirectoryCacheCount: ipfs.DefaultDirCacheCount,
		LinkLimit:           ipfs.DefaultLinkLimit,
	}
	ipfsBindAPIMaddrFlag(
		flagSet, system, prefix,
		settings.newSetFunc(ipfs.APIAttribute),
	)
	ipfsBindAPITimeoutFlag(
		flagSet, prefix, settings.APITimeout,
		settings.newSetFunc(ipfs.APITimeoutAttribute),
	)
	ipfsBindLinkLimitFlag(
		flagSet, prefix, settings.LinkLimit,
		settings.newSetFunc(ipfs.LinkLimitAttribute),
	)
	settings.bindPermissionsFlag(flagSet)
	settings.bindNodeCacheFlag(flagSet)
	settings.bindDirCacheFlag(flagSet)
}

func (settings *ipfsSettings) newSetFunc(attribute string) flagSetFunc {
	return func(argument string) error {
		return (*ipfs.FSMaker)(settings).ParseField(attribute, argument)
	}
}

func (settings *ipfsSettings) bindNodeCacheFlag(flagSet *flag.FlagSet) {
	const (
		prefix = "ipfs-"
		name   = prefix + "node-cache"
		usage  = "number of nodes to keep in the cache" +
			"\nnegative values disable node caching"
	)
	setFlagOnce[int](
		flagSet, name, usage,
		settings.newSetFunc(ipfs.NodeCacheAttribute),
	)
	flagSet.Lookup(name).
		DefValue = strconv.Itoa(settings.NodeCacheCount)
}

func (settings *ipfsSettings) bindDirCacheFlag(flagSet *flag.FlagSet) {
	const (
		prefix = "ipfs-"
		name   = prefix + "directory-cache"
		usage  = "number of directory entry lists to keep in the cache" +
			"\nnegative values disable directory caching"
	)
	setFlagOnce[int](
		flagSet, name, usage,
		settings.newSetFunc(ipfs.DirectoryCacheAttribute),
	)
	flagSet.Lookup(name).
		DefValue = strconv.Itoa(settings.DirectoryCacheCount)
}

func (settings *ipfsSettings) bindPermissionsFlag(flagSet *flag.FlagSet) {
	const (
		prefix  = "ipfs-"
		whatFor = "UFSv1 file metadata"
	)
	ipfsBindPermissionsFlag(
		flagSet, prefix, whatFor, settings.Permissions,
		settings.newSetFunc(ipfs.PermissionsAttribute),
	)
}

func (settings *ipfsSettings) make() (mountpoint.Guest, error) {
	return (*ipfs.FSMaker)(settings), nil
}
