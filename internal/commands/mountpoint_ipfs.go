//go:build !noipfs

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/ipfs"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	giconfig "github.com/ipfs/kubo/config"
	"github.com/multiformats/go-multiaddr"
)

type (
	ipfsSettings  ipfs.IPFSGuest
	ipfsOption    func(*ipfsSettings) error
	ipfsOptions   []ipfsOption
	pinFSSettings ipfs.PinFSGuest
	pinFSOption   func(*pinFSSettings) error
	pinFSOptions  []pinFSOption
	ipnsSettings  ipfs.IPNSGuest
	ipnsOption    func(*ipnsSettings) error
	ipnsOptions   []ipnsOption
	keyFSSettings ipfs.KeyFSGuest
	keyFSOption   func(*keyFSSettings) error
	keyFSOptions  []keyFSOption
)

const (
	ipfsAPIFileName    = "api"
	ipfsConfigEnv      = giconfig.EnvDir
	ipfsConfigDir      = giconfig.DefaultPathRoot
	defaultPinfsExpiry = 30 * time.Second
	defaultIPNSExpiry  = 1 * time.Minute
)

func makeIPFSCommands[
	HC mountCmdHost[HT, HM],
	HM marshaller,
	HT any,
](host filesystem.Host,
) []command.Command {
	return []command.Command{
		makeMountCommand[HC, HM, ipfsOptions, ipfsSettings](host, ipfs.IPFSID),
		makeMountCommand[HC, HM, pinFSOptions, pinFSSettings](host, ipfs.PinFSID),
		makeMountCommand[HC, HM, ipnsOptions, ipnsSettings](host, ipfs.IPNSID),
		makeMountCommand[HC, HM, keyFSOptions, keyFSSettings](host, ipfs.KeyFSID),
	}
}

func makeIPFSGuests[
	HC mountPointHost[T],
	T any,
](guests mountPointGuests, path ninePath,
) {
	guests[ipfs.IPFSID] = newMountPointFunc[HC, ipfs.IPFSGuest](path)
	guests[ipfs.IPNSID] = newMountPointFunc[HC, ipfs.IPNSGuest](path)
	guests[ipfs.KeyFSID] = newMountPointFunc[HC, ipfs.KeyFSGuest](path)
	guests[ipfs.PinFSID] = newMountPointFunc[HC, ipfs.PinFSGuest](path)
}

func guestOverlayText(overlay, overlaid filesystem.ID) string {
	return string(overlay) + " is an " + string(overlaid) + " overlay"
}

func (*ipfsOptions) usage(filesystem.Host) string {
	return string(ipfs.IPFSID) + " provides an empty root directory." +
		"\nChild paths are forwarded to the IPFS API."
}

func (io *ipfsOptions) BindFlags(flagSet *flag.FlagSet) {
	io.bindFlagsVarient(ipfs.IPFSID, flagSet)
}

func (io *ipfsOptions) bindFlagsVarient(system filesystem.ID, flagSet *flag.FlagSet) {
	io.bindAPIFlag(system, flagSet)
	io.bindTimeoutFlag(system, flagSet)
	io.bindNodeCacheFlag(system, flagSet)
	io.bindDirCacheFlag(system, flagSet)
}

func (io *ipfsOptions) bindAPIFlag(system filesystem.ID, flagSet *flag.FlagSet) {
	var (
		prefix = prefixIDFlag(system)
		usage  = string(system) + " API node `maddr`"
		name   = prefix + ipfsAPIFileName
	)
	getRefFn := func(settings *ipfsSettings) *multiaddr.Multiaddr {
		return &settings.APIMaddr
	}
	appendFlagValue(flagSet, name, usage, io,
		multiaddr.NewMultiaddr, getRefFn)
	flagSet.Lookup(name).
		DefValue = fmt.Sprintf(
		"parses: %s, %s",
		filepath.Join("$"+ipfsConfigEnv, ipfsAPIFileName),
		filepath.Join(ipfsConfigDir, ipfsAPIFileName),
	)
}

func (io *ipfsOptions) bindTimeoutFlag(system filesystem.ID, flagSet *flag.FlagSet) {
	const usage = "timeout `duration` to use when communicating" +
		" with the API" +
		"\nif <= 0, operations will remain pending" +
		" until the operation completes, or the file or system is closed"
	var (
		prefix   = prefixIDFlag(system)
		name     = prefix + "timeout"
		getRefFn = func(settings *ipfsSettings) *time.Duration {
			return &settings.APITimeout
		}
	)
	appendFlagValue(flagSet, name, usage,
		io, time.ParseDuration, getRefFn,
	)
}

func (io *ipfsOptions) bindNodeCacheFlag(system filesystem.ID, flagSet *flag.FlagSet) {
	const usage = "number of nodes to keep in the cache" +
		"\nnegative values disable node caching"
	var (
		prefix  = prefixIDFlag(system)
		name    = prefix + "node-cache"
		parseFn = func(argument string) (int, error) {
			const (
				base    = 0
				bitSize = 0
			)
			count, err := strconv.ParseInt(argument, base, bitSize)
			return int(count), err
		}
		getRefFn = func(settings *ipfsSettings) *int {
			return &settings.NodeCacheCount
		}
	)
	appendFlagValue(flagSet, name, usage,
		io, parseFn, getRefFn)
}

func (io *ipfsOptions) bindDirCacheFlag(system filesystem.ID, flagSet *flag.FlagSet) {
	const usage = "number of directory entry lists to keep in the cache" +
		"\nnegative values disable directory caching"
	var (
		prefix  = prefixIDFlag(system)
		name    = prefix + "directory-cache"
		parseFn = func(argument string) (int, error) {
			const (
				base    = 0
				bitSize = 0
			)
			count, err := strconv.ParseInt(argument, base, bitSize)
			return int(count), err
		}
		getRefFn = func(settings *ipfsSettings) *int {
			return &settings.DirectoryCacheCount
		}
	)
	appendFlagValue(flagSet, name, usage,
		io, parseFn, getRefFn)
}

func (io ipfsOptions) make() (ipfsSettings, error) {
	settings, err := makeWithOptions(io...)
	if err != nil {
		return ipfsSettings{}, err
	}
	if settings.APIMaddr == nil {
		maddrs, err := getIPFSAPI()
		if err != nil {
			return ipfsSettings{}, fmt.Errorf(
				"could not get default value for API: %w",
				err,
			)
		}
		settings.APIMaddr = maddrs[0]
	}
	return settings, nil
}

func (set ipfsSettings) marshal(string) ([]byte, error) {
	return json.Marshal(set)
}

func (*pinFSOptions) usage(filesystem.Host) string {
	return guestOverlayText(ipfs.PinFSID, ipfs.IPFSID) +
		" which provides a root containing" +
		"\nentries for each recursive pin from the IPFS node." +
		"\nChild paths are forwarded to IPFS."
}

func (po *pinFSOptions) BindFlags(flagSet *flag.FlagSet) {
	po.bindIPFSFlags(flagSet)
	po.bindExpiryFlag(flagSet)
}

func (po *pinFSOptions) bindIPFSFlags(flagSet *flag.FlagSet) {
	var ipfsOptions ipfsOptions
	(&ipfsOptions).bindFlagsVarient(ipfs.PinFSID, flagSet)
	*po = append(*po, func(settings *pinFSSettings) error {
		subset, err := ipfsOptions.make()
		if err != nil {
			return err
		}
		settings.IPFSGuest = ipfs.IPFSGuest(subset)
		return nil
	})
}

func (po *pinFSOptions) bindExpiryFlag(flagSet *flag.FlagSet) {
	const (
		name  = "pinfs-expiry"
		usage = "`duration` pins are cached for" +
			"\nnegative values retain cache forever, 0 disables cache"
	)
	getRefFn := func(settings *pinFSSettings) *time.Duration {
		return &settings.CacheExpiry
	}
	appendFlagValue(flagSet, name, usage,
		po, time.ParseDuration, getRefFn)
	flagSet.Lookup(name).
		DefValue = defaultPinfsExpiry.String()
}

func (po pinFSOptions) make() (pinFSSettings, error) {
	settings := pinFSSettings{
		CacheExpiry: defaultPinfsExpiry,
	}
	return settings, generic.ApplyOptions(&settings, po...)
}

func (set pinFSSettings) marshal(string) ([]byte, error) {
	return json.Marshal(set)
}

func (*ipnsOptions) usage(filesystem.Host) string {
	return guestOverlayText(ipfs.IPNSID, ipfs.IPFSID) +
		" which provides an empty root." +
		"\nThe first element in a child path is resolved via IPNS." +
		"\nSubsequent paths are forwarded to IPFS (rooted under the resolved IPNS name)."
}

func (no *ipnsOptions) BindFlags(flagSet *flag.FlagSet) {
	no.bindFlagsVarient(ipfs.IPNSID, flagSet)
}

func (no *ipnsOptions) bindFlagsVarient(system filesystem.ID, flagSet *flag.FlagSet) {
	no.bindIPFSFlags(system, flagSet)
	no.bindExpiryFlag(system, flagSet)
}

func (no *ipnsOptions) bindIPFSFlags(system filesystem.ID, flagSet *flag.FlagSet) {
	var ipfsOptions ipfsOptions
	(&ipfsOptions).bindFlagsVarient(system, flagSet)
	*no = append(*no, func(settings *ipnsSettings) error {
		subset, err := ipfsOptions.make()
		if err != nil {
			return err
		}
		settings.IPFSGuest = ipfs.IPFSGuest(subset)
		return nil
	})
}

func (no *ipnsOptions) bindExpiryFlag(system filesystem.ID, flagSet *flag.FlagSet) {
	var (
		prefix = prefixIDFlag(system)
		name   = prefix + "expiry"
	)
	const (
		usage = "`duration` of how long a node is considered" +
			" valid within the cache" +
			"\nafter this time, the node will be refreshed during" +
			" its next operation"
	)
	getRefFn := func(settings *ipnsSettings) *time.Duration {
		return &settings.NodeExpiry
	}
	appendFlagValue(flagSet, name, usage,
		no, time.ParseDuration, getRefFn)
	flagSet.Lookup(name).
		DefValue = defaultIPNSExpiry.String()
}

func (no ipnsOptions) make() (ipnsSettings, error) {
	settings := ipnsSettings{
		NodeExpiry: defaultIPNSExpiry,
	}
	return settings, generic.ApplyOptions(&settings, no...)
}

func (set ipnsSettings) marshal(string) ([]byte, error) {
	return json.Marshal(set)
}

func (*keyFSOptions) usage(filesystem.Host) string {
	return guestOverlayText(ipfs.KeyFSID, ipfs.IPNSID) +
		" which provides a root" +
		"\ncontaining entries for each IPNS key from the IPFS node." +
		"\nChild paths are forwarded to IPNS after being resolved."
}

func (ko *keyFSOptions) BindFlags(flagSet *flag.FlagSet) {
	var ipnsOptions ipnsOptions
	(&ipnsOptions).bindFlagsVarient(ipfs.KeyFSID, flagSet)
	*ko = append(*ko, func(settings *keyFSSettings) error {
		subset, err := ipnsOptions.make()
		if err != nil {
			return err
		}
		settings.IPNSGuest = ipfs.IPNSGuest(subset)
		return nil
	})
}

func (ko keyFSOptions) make() (keyFSSettings, error) {
	return makeWithOptions(ko...)
}

func (set keyFSSettings) marshal(string) ([]byte, error) {
	return json.Marshal(set)
}

func getIPFSAPI() ([]multiaddr.Multiaddr, error) {
	location, err := getIPFSAPIPath()
	if err != nil {
		return nil, err
	}
	if !apiFileExists(location) {
		return nil, generic.ConstError("IPFS API file not found")
	}
	return parseIPFSAPI(location)
}

func getIPFSAPIPath() (string, error) {
	var target string
	if ipfsPath, set := os.LookupEnv(ipfsConfigEnv); set {
		target = filepath.Join(ipfsPath, ipfsAPIFileName)
	} else {
		target = filepath.Join(ipfsConfigDir, ipfsAPIFileName)
	}
	return expandHomeShorthand(target)
}

func expandHomeShorthand(name string) (string, error) {
	if !strings.HasPrefix(name, "~") {
		return name, nil
	}
	homeName, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeName, name[1:]), nil
}

func apiFileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

func parseIPFSAPI(name string) ([]multiaddr.Multiaddr, error) {
	// NOTE: [upstream problem]
	// If the config file has multiple API maddrs defined,
	// only the first one will be contained in the API file.
	maddrString, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	maddr, err := multiaddr.NewMultiaddr(string(maddrString))
	if err != nil {
		return nil, err
	}
	return []multiaddr.Multiaddr{maddr}, nil
}
