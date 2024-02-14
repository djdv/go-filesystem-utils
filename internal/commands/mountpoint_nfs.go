//go:build !nonfs

package commands

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"

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
	no.bindServerFlag(flagSet)
	no.bindHostnameFlag(flagSet)
	no.bindDirpathFlag(flagSet)
	no.bindLinkSepFlag(flagSet)
	no.bindLinkLimitFlag(flagSet)
	no.bindUIDFlag(flagSet)
	no.bindGIDFlag(flagSet)
}

func (no *nfsGuestOptions) bindServerFlag(flagSet *flag.FlagSet) {
	const usage = "NFS server `maddr`"
	var (
		prefix   = prefixIDFlag(nfs.GuestID)
		name     = prefix + nfsServerFlagName
		getRefFn = func(settings *nfsGuestSettings) *multiaddr.Multiaddr {
			return &settings.Maddr
		}
	)
	appendFlagValue(flagSet, name, usage,
		no, multiaddr.NewMultiaddr, getRefFn)
}

func (no *nfsGuestOptions) bindHostnameFlag(flagSet *flag.FlagSet) {
	const usage = "client's `hostname`"
	var (
		prefix   = prefixIDFlag(nfs.GuestID)
		name     = prefix + "hostname"
		getRefFn = func(settings *nfsGuestSettings) *string {
			return &settings.Hostname
		}
		parseFn = newPassthroughFunc(name)
	)
	appendFlagValue(flagSet, name, usage,
		no, parseFn, getRefFn)
	flagSet.Lookup(name).
		DefValue = "caller's hostname"
}

func (no *nfsGuestOptions) bindDirpathFlag(flagSet *flag.FlagSet) {
	const usage = "`dirpath` used when mounting the server"
	var (
		prefix   = prefixIDFlag(nfs.GuestID)
		name     = prefix + "dirpath"
		getRefFn = func(settings *nfsGuestSettings) *string {
			return &settings.Dirpath
		}
		parseFn = newPassthroughFunc(name)
	)
	appendFlagValue(flagSet, name, usage,
		no, parseFn, getRefFn)
	flagSet.Lookup(name).
		DefValue = "/"
}

func (no *nfsGuestOptions) bindLinkSepFlag(flagSet *flag.FlagSet) {
	const usage = "`separator` character to replace with `/` when parsing relative symlinks"
	var (
		prefix   = prefixIDFlag(nfs.GuestID)
		name     = prefix + "link-separator"
		getRefFn = func(settings *nfsGuestSettings) *string {
			return &settings.LinkSeparator
		}
		parseFn = newPassthroughFunc(name)
	)
	appendFlagValue(flagSet, name, usage,
		no, parseFn, getRefFn)
}

func (no *nfsGuestOptions) bindLinkLimitFlag(flagSet *flag.FlagSet) {
	const usage = "sets the maximum amount of times a symbolic link will be resolved in a link chain"
	var (
		prefix   = prefixIDFlag(nfs.GuestID)
		name     = prefix + "link-limit"
		getRefFn = func(settings *nfsGuestSettings) *uint {
			return &settings.LinkLimit
		}
		parseFn = func(argument string) (uint, error) {
			const (
				base    = 0
				bitSize = 0
			)
			limit, err := strconv.ParseUint(argument, base, bitSize)
			return uint(limit), err
		}
	)
	appendFlagValue(flagSet, name, usage,
		no, parseFn, getRefFn)
}

func (no *nfsGuestOptions) bindUIDFlag(flagSet *flag.FlagSet) {
	const usage = "client's `uid`"
	var (
		prefix   = prefixIDFlag(nfs.GuestID)
		name     = prefix + "uid"
		getRefFn = func(settings *nfsGuestSettings) *uint32 {
			return &settings.UID
		}
		parseFn = func(argument string) (uint32, error) {
			const (
				base    = 0
				bitSize = 32
			)
			id, err := strconv.ParseUint(argument, base, bitSize)
			return uint32(id), err
		}
	)
	appendFlagValue(flagSet, name, usage,
		no, parseFn, getRefFn)
	flagSet.Lookup(name).
		DefValue = "caller's uid"
}

func (no *nfsGuestOptions) bindGIDFlag(flagSet *flag.FlagSet) {
	const usage = "client's `gid`"
	var (
		prefix   = prefixIDFlag(nfs.GuestID)
		name     = prefix + "gid"
		getRefFn = func(settings *nfsGuestSettings) *uint32 {
			return &settings.GID
		}
		parseFn = func(argument string) (uint32, error) {
			const (
				base    = 0
				bitSize = 32
			)
			id, err := strconv.ParseUint(argument, base, bitSize)
			return uint32(id), err
		}
	)
	appendFlagValue(flagSet, name, usage,
		no, parseFn, getRefFn)
	flagSet.Lookup(name).
		DefValue = "caller's gid"
}

func (no nfsGuestOptions) make() (nfsGuestSettings, error) {
	settings, err := makeWithOptions(no...)
	if err != nil {
		return nfsGuestSettings{}, err
	}
	if settings.Maddr == nil {
		var (
			prefix  = prefixIDFlag(nfs.GuestID)
			srvName = prefix + nfsServerFlagName
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
