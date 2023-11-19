//go:build !nonfs

package commands

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/nfs"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/multiformats/go-multiaddr"
)

type (
	nfsHostSettings  nfs.Host
	nfsHostOption    func(*nfsHostSettings) error
	nfsHostOptions   []nfsHostOption
	nfsGuestSettings struct {
		nfs.Guest
		defaultUID, defaultGID,
		defaultHostname, defaultDirpath bool
	}
	nfsGuestOption  func(*nfsGuestSettings) error
	nfsGuestOptions []nfsGuestOption
)

const nfsServerFlagName = "server"

func (ns nfsGuestSettings) marshal(string) ([]byte, error) {
	return json.Marshal(ns.Guest)
}

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

func makeNFSGuestCommand[
	HC mountCmdHost[HT, HM],
	HM marshaller,
	HT any,
](host filesystem.Host,
) command.Command {
	return makeMountCommand[HC, HM, nfsGuestOptions, nfsGuestSettings](host, nfs.GuestID)
}

func makeNFSGuest[
	HC mountPointHost[T],
	T any,
](guests mountPointGuests, path ninePath,
) {
	guests[nfs.GuestID] = newMountPointFunc[HC, nfs.Guest](path)
}

func (*nfsGuestOptions) usage(filesystem.Host) string {
	return string(nfs.GuestID) + " attaches to an NFS file server"
}

func (no *nfsGuestOptions) BindFlags(flagSet *flag.FlagSet) {
	var (
		flagPrefix = prefixIDFlag(nfs.GuestID)
		srvName    = flagPrefix + nfsServerFlagName
	)
	const srvUsage = "NFS server `maddr`"
	flagSetFunc(flagSet, srvName, srvUsage, no,
		func(value multiaddr.Multiaddr, settings *nfsGuestSettings) error {
			settings.Maddr = value
			return nil
		})
	hostnameName := flagPrefix + "hostname"
	const hostnameUsage = "client's `hostname`"
	flagSetFunc(flagSet, hostnameName, hostnameUsage, no,
		func(value string, settings *nfsGuestSettings) error {
			settings.Hostname = value
			return nil
		})
	flagSet.Lookup(hostnameName).
		DefValue = "caller's hostname"
	dirpathName := flagPrefix + "dirpath"
	const dirpathUsage = "`dirpath` used when mounting the server"
	flagSetFunc(flagSet, dirpathName, dirpathUsage, no,
		func(value string, settings *nfsGuestSettings) error {
			settings.Dirpath = value
			return nil
		})
	flagSet.Lookup(dirpathName).
		DefValue = "/"
	linkSepName := flagPrefix + "link-separator"
	const linkSepUsage = "`separator` character to replace with `/` when parsing relative symlinks"
	flagSetFunc(flagSet, linkSepName, linkSepUsage, no,
		func(value string, settings *nfsGuestSettings) error {
			settings.LinkSeparator = value
			return nil
		})
	linkLimitName := flagPrefix + "link-limit"
	const linkLimitUsage = "sets the maximum amount of times a symbolic link will be resolved in a link chain"
	flagSetFunc(flagSet, linkLimitName, linkLimitUsage, no,
		func(value uint, settings *nfsGuestSettings) error {
			settings.LinkLimit = value
			return nil
		})
	uidName := flagPrefix + "uid"
	const uidUsage = "client's `uid`"
	flagSetFunc(flagSet, uidName, uidUsage, no,
		func(value uint32, settings *nfsGuestSettings) error {
			settings.UID = value
			return nil
		})
	flagSet.Lookup(uidName).
		DefValue = "caller's uid"
	gidName := flagPrefix + "gid"
	const gidUsage = "client's `gid`"
	flagSetFunc(flagSet, gidName, gidUsage, no,
		func(value uint32, settings *nfsGuestSettings) error {
			settings.GID = value
			return nil
		})
	flagSet.Lookup(gidName).
		DefValue = "caller's gid"
}

func (no nfsGuestOptions) make() (nfsGuestSettings, error) {
	settings, err := makeWithOptions(no...)
	if err != nil {
		return nfsGuestSettings{}, err
	}
	if settings.Maddr == nil {
		var (
			flagPrefix = prefixIDFlag(nfs.GuestID)
			srvName    = flagPrefix + nfsServerFlagName
		)
		return nfsGuestSettings{}, fmt.Errorf(
			"flag `-%s` must be provided for NFS guests",
			srvName,
		)
	}
	if settings.Hostname == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nfsGuestSettings{}, err
		}
		settings.Hostname = hostname
	}
	if settings.defaultUID {
		settings.UID = uint32(os.Getuid())
	}
	if settings.defaultGID {
		settings.GID = uint32(os.Getgid())
	}
	if settings.Dirpath == "" {
		settings.Dirpath = "/"
	}
	return settings, nil
}
