//go:build !noipfs

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
	ipfsAPIFileName      = "api"
	ipfsConfigEnv        = giconfig.EnvDir
	ipfsConfigDefaultDir = giconfig.DefaultPathRoot
	pinfsExpiryDefault   = 30 * time.Second
	ipnsExpiryDefault    = 1 * time.Minute
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

func prefixIDFlag(system filesystem.ID) string {
	return strings.ToLower(string(system)) + "-"
}

func (*ipfsOptions) usage(filesystem.Host) string {
	return string(ipfs.IPFSID) + " provides an empty root directory." +
		"\nChild paths are forwarded to the IPFS API."
}

func (io *ipfsOptions) BindFlags(flagSet *flag.FlagSet) {
	io.bindFlagsVarient(ipfs.IPFSID, flagSet)
}

func (io *ipfsOptions) bindFlagsVarient(system filesystem.ID, flagSet *flag.FlagSet) {
	var (
		flagPrefix = prefixIDFlag(system)
		apiUsage   = string(system) + " API node `maddr`"
		apiName    = flagPrefix + ipfsAPIFileName
	)
	flagSetFunc(flagSet, apiName, apiUsage, io,
		func(value multiaddr.Multiaddr, settings *ipfsSettings) error {
			settings.APIMaddr = value
			return nil
		})
	flagSet.Lookup(apiName).
		DefValue = fmt.Sprintf(
		"parses: %s, %s",
		filepath.Join("$"+ipfsConfigEnv, ipfsAPIFileName),
		filepath.Join(ipfsConfigDefaultDir, ipfsAPIFileName),
	)
	const timeoutUsage = "timeout `duration` to use when communicating" +
		" with the API" +
		"\nif <= 0, operations will remain pending" +
		" until the operation completes, or the file or system is closed"
	timeoutName := flagPrefix + "timeout"
	flagSetFunc(flagSet, timeoutName, timeoutUsage, io,
		func(value time.Duration, settings *ipfsSettings) error {
			settings.APITimeout = value
			return nil
		})
	nodeCacheName := flagPrefix + "node-cache"
	const nodeCacheUsage = "number of nodes to keep in the cache" +
		"\nnegative values disable node caching"
	flagSetFunc(flagSet, nodeCacheName, nodeCacheUsage, io,
		func(value int, settings *ipfsSettings) error {
			settings.NodeCacheCount = value
			return nil
		})
	dirCacheName := flagPrefix + "directory-cache"
	const dirCacheUsage = "number of directory entry lists to keep in the cache" +
		"\nnegative values disable directory caching"
	flagSetFunc(flagSet, dirCacheName, dirCacheUsage, io,
		func(value int, settings *ipfsSettings) error {
			settings.DirectoryCacheCount = value
			return nil
		})
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
	const (
		expiryName  = "pinfs-expiry"
		expiryUsage = "`duration` pins are cached for" +
			"\nnegative values retain cache forever, 0 disables cache"
	)
	flagSetFunc(flagSet, expiryName, expiryUsage, po,
		func(value time.Duration, settings *pinFSSettings) error {
			settings.CacheExpiry = value
			return nil
		})
	flagSet.Lookup(expiryName).
		DefValue = pinfsExpiryDefault.String()
}

func (po pinFSOptions) make() (pinFSSettings, error) {
	settings := pinFSSettings{
		CacheExpiry: pinfsExpiryDefault,
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
	var (
		flagPrefix = prefixIDFlag(system)
		expiryName = flagPrefix + "expiry"
	)
	const (
		expiryUsage = "`duration` of how long a node is considered" +
			"valid within the cache" +
			"\nafter this time, the node will be refreshed during" +
			" its next operation"
	)
	flagSetFunc(flagSet, expiryName, expiryUsage, no,
		func(value time.Duration, settings *ipnsSettings) error {
			settings.NodeExpiry = value
			return nil
		})
	flagSet.Lookup(expiryName).
		DefValue = ipnsExpiryDefault.String()
}

func (no ipnsOptions) make() (ipnsSettings, error) {
	settings := ipnsSettings{
		NodeExpiry: ipnsExpiryDefault,
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
		target = filepath.Join(ipfsConfigDefaultDir, ipfsAPIFileName)
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
