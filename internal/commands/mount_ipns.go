//go:build !noipfs

package commands

import (
	"flag"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/ipfs"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/ipns"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
)

// ipnsSettings adapts [ipns.FSMaker]
// for use with the CLI. Particularly
// related to [flag] and [command.Command].
type ipnsSettings ipns.FSMaker

const ipnsID = ipns.ID

func (*ipnsSettings) ID() filesystem.ID { return ipnsID }

func (*ipnsSettings) usage(filesystem.Host) string {
	return guestOverlayText(ipnsID, ipfsID) +
		" which provides an empty root." +
		"\nRoot entry paths are resolved via IPNS." +
		"\nNested paths are forwarded to IPFS after being resolved."
}

func (settings *ipnsSettings) BindFlags(flagSet *flag.FlagSet) {
	const (
		system = ipnsID
		prefix = "ipns-"
	)
	*settings = ipnsSettings{
		IPFS:        new(ipfs.FSMaker),
		APITimeout:  ipns.DefaultAPITimeout,
		Permissions: ipns.DefaultPermissions,
		CacheExpiry: ipns.DefaultCacheExpiry,
		LinkLimit:   ipns.DefaultLinkLimit,
	}
	(*ipfsSettings)(settings.IPFS).BindFlags(flagSet)
	ipfsBindAPIMaddrFlag(
		flagSet, system, prefix,
		settings.newSetFunc(ipns.APIAttribute),
	)
	ipfsBindAPITimeoutFlag(
		flagSet, prefix, settings.APITimeout,
		settings.newSetFunc(ipns.APITimeoutAttribute),
	)
	ipfsBindLinkLimitFlag(
		flagSet, prefix, settings.LinkLimit,
		settings.newSetFunc(ipns.LinkLimitAttribute),
	)
	settings.bindPermissionsFlag(prefix, flagSet)
	settings.bindCacheExpiryFlag(prefix, flagSet)
}

func (settings *ipnsSettings) newSetFunc(attribute string) flagSetFunc {
	return func(argument string) error {
		return (*ipns.FSMaker)(settings).ParseField(attribute, argument)
	}
}

func (settings *ipnsSettings) bindPermissionsFlag(prefix string, flagSet *flag.FlagSet) {
	const whatFor = "IPNS root directory"
	ipfsBindPermissionsFlag(
		flagSet, prefix, whatFor, settings.Permissions,
		settings.newSetFunc(ipns.PermissionsAttribute),
	)
}

func (settings *ipnsSettings) bindCacheExpiryFlag(prefix string, flagSet *flag.FlagSet) {
	const usage = "sets how long a node is considered valid" +
		" within the cache; after this time, the node will be" +
		" refreshed during its next operation" +
		"\n(0 disables caching, <0 cache entires never get invalidated)"
	name := prefix + "cache-expiry"
	setFlagOnce[time.Duration](
		flagSet, name, usage,
		settings.newSetFunc(ipns.ExpiryAttribute),
	)
	flagSet.Lookup(name).
		DefValue = settings.CacheExpiry.String()
}

func (settings *ipnsSettings) make() (mountpoint.Guest, error) {
	ipfsSet, err := (*ipfsSettings)(settings.IPFS).make()
	if err != nil {
		return nil, err
	}
	settings.IPFS = ipfsSet.(*ipfs.FSMaker)
	return (*ipns.FSMaker)(settings), nil
}
