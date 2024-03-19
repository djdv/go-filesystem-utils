//go:build !noipfs

package commands

import (
	"flag"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/ipfs"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/ipns"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/keyfs"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
)

type keyfsSettings keyfs.FSMaker

const keyfsID = keyfs.ID

func (*keyfsSettings) ID() filesystem.ID { return keyfsID }

func (*keyfsSettings) usage(filesystem.Host) string {
	return guestOverlayText(keyfsID, ipnsID) +
		" which provides a root containing" +
		" entries for each IPNS key from the IPFS node." +
		"\nPaths are forwarded to IPNS after being resolved."
}

func (settings *keyfsSettings) BindFlags(flagSet *flag.FlagSet) {
	const (
		system = keyfsID
		prefix = "keyfs-"
	)
	*settings = keyfsSettings{
		IPNS:        new(ipns.FSMaker),
		APITimeout:  keyfs.DefaultAPITimeout,
		Permissions: keyfs.DefaultPermissions,
		CacheExpiry: keyfs.DefaultCacheExpiry,
		LinkLimit:   keyfs.DefaultLinkLimit,
	}
	(*ipnsSettings)(settings.IPNS).BindFlags(flagSet)
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
	settings.bindPermissionsFlag(prefix, flagSet)
	settings.bindCacheExpiryFlag(prefix, flagSet)
}

func (settings *keyfsSettings) newSetFunc(attribute string) flagSetFunc {
	return func(argument string) error {
		return (*keyfs.FSMaker)(settings).ParseField(attribute, argument)
	}
}

func (settings *keyfsSettings) bindPermissionsFlag(prefix string, flagSet *flag.FlagSet) {
	const whatFor = "keys root directory"
	ipfsBindPermissionsFlag(
		flagSet, prefix, whatFor, settings.Permissions,
		settings.newSetFunc(keyfs.PermissionsAttribute),
	)
}

func (settings *keyfsSettings) bindCacheExpiryFlag(prefix string, flagSet *flag.FlagSet) {
	const usage = "sets how long the keys cache is considered valid" +
		"\n(0 disables caching, <0 cache entires never get invalidated)"
	name := prefix + "cache-expiry"
	setFlagOnce[time.Duration](
		flagSet, name, usage,
		settings.newSetFunc(keyfs.ExpiryAttribute),
	)
	flagSet.Lookup(name).
		DefValue = settings.CacheExpiry.String()
}

func (settings *keyfsSettings) make() (mountpoint.Guest, error) {
	ipnsSet, err := (*ipnsSettings)(settings.IPNS).make()
	if err != nil {
		return nil, err
	}
	settings.IPNS = ipnsSet.(*ipns.FSMaker)
	return (*keyfs.FSMaker)(settings), nil
}
