//go:build !noipfs

package commands

import (
	"flag"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/ipfs"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/pinfs"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
)

type pinfsSettings pinfs.FSMaker

const pinfsID = pinfs.ID

func (*pinfsSettings) ID() filesystem.ID { return pinfsID }

func (*pinfsSettings) usage(filesystem.Host) string {
	return guestOverlayText(pinfsID, ipfsID) +
		" which provides a root containing" +
		" entries for each recursive pin on the IPFS node." +
		"\nPaths are forwarded to IPFS after being resolved."
}

func (settings *pinfsSettings) BindFlags(flagSet *flag.FlagSet) {
	const (
		system = pinfsID
		prefix = "pinfs-"
	)
	*settings = pinfsSettings{
		IPFS:        new(ipfs.FSMaker),
		APITimeout:  pinfs.DefaultAPITimeout,
		Permissions: pinfs.DefaultPermissions,
		CacheExpiry: pinfs.DefaultCacheExpiry,
	}
	(*ipfsSettings)(settings.IPFS).BindFlags(flagSet)
	ipfsBindAPIMaddrFlag(
		flagSet, system, prefix,
		settings.newSetFunc(pinfs.APIAttribute),
	)
	ipfsBindAPITimeoutFlag(
		flagSet, prefix, settings.APITimeout,
		settings.newSetFunc(pinfs.APITimeoutAttribute),
	)
	settings.bindPermissionsFlag(prefix, flagSet)
	settings.bindCacheExpiryFlag(prefix, flagSet)
}

func (settings *pinfsSettings) newSetFunc(attribute string) flagSetFunc {
	return func(argument string) error {
		return (*pinfs.FSMaker)(settings).ParseField(attribute, argument)
	}
}

func (settings *pinfsSettings) bindPermissionsFlag(prefix string, flagSet *flag.FlagSet) {
	const whatFor = "pins root directory"
	ipfsBindPermissionsFlag(
		flagSet, prefix, whatFor, settings.Permissions,
		settings.newSetFunc(pinfs.PermissionsAttribute),
	)
}

func (settings *pinfsSettings) bindCacheExpiryFlag(prefix string, flagSet *flag.FlagSet) {
	const usage = "sets how long the pin set cache is considered valid" +
		"\n(0 disables caching, <0 cache entires never get invalidated)"
	name := prefix + "cache-expiry"
	setFlagOnce[time.Duration](
		flagSet, name, usage,
		settings.newSetFunc(pinfs.ExpiryAttribute),
	)
	flagSet.Lookup(name).
		DefValue = settings.CacheExpiry.String()
}

func (settings *pinfsSettings) make() (mountpoint.Guest, error) {
	ipfsSet, err := (*ipfsSettings)(settings.IPFS).make()
	if err != nil {
		return nil, err
	}
	settings.IPFS = ipfsSet.(*ipfs.FSMaker)
	return (*pinfs.FSMaker)(settings), nil
}
