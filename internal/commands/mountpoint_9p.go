package commands

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/multiformats/go-multiaddr"
)

type (
	plan9GuestSettings p9fs.Guest
	plan9GuestOption   func(*plan9GuestSettings) error
	plan9GuestOptions  []plan9GuestOption

	plan9HostSettings p9fs.Host
	plan9HostOption   func(*plan9HostSettings) error
	plan9HostOptions  []plan9HostOption
)

const p9ServerFlagName = "server"

func makePlan9HostCommand() command.Command {
	return makeMountSubcommand(
		p9fs.HostID,
		makeGuestCommands[plan9HostOptions, plan9HostSettings](p9fs.HostID),
	)
}

func makePlan9Host(path ninePath, autoUnlink bool) (filesystem.Host, p9fs.MakeGuestFunc) {
	guests := makeMountPointGuests[p9fs.Host](path)
	return p9fs.HostID, newMakeGuestFunc(guests, path, autoUnlink)
}

func unmarshalPlan9() (filesystem.Host, decodeFunc) {
	return p9fs.HostID, func(b []byte) (string, error) {
		var host p9fs.Host
		err := json.Unmarshal(b, &host)
		return host.Maddr.String(), err
	}
}

func (*plan9HostOptions) usage(guest filesystem.ID) string {
	return string(p9fs.HostID) + " hosts " +
		string(guest) + " as a 9P file server"
}

func (*plan9HostOptions) BindFlags(*flag.FlagSet) { /* NOOP */ }

func (o9 plan9HostOptions) make() (plan9HostSettings, error) {
	return makeWithOptions(o9...)
}

func (set plan9HostSettings) marshal(arg string) ([]byte, error) {
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

func makePlan9GuestCommand[
	HC mountCmdHost[HT, HM],
	HM marshaller,
	HT any,
](host filesystem.Host,
) command.Command {
	return makeMountCommand[HC, HM, plan9GuestOptions, plan9GuestSettings](host, p9fs.GuestID)
}

func (*plan9GuestOptions) usage(filesystem.Host) string {
	return string(p9fs.GuestID) + " attaches to a 9P file server"
}

func (o9 *plan9GuestOptions) BindFlags(flagSet *flag.FlagSet) {
	var (
		flagPrefix = prefixIDFlag(p9fs.GuestID)
		srvUsage   = "9P2000.L file system server `maddr`"
		srvName    = flagPrefix + p9ServerFlagName
	)
	flagSetFunc(flagSet, srvName, srvUsage, o9,
		func(value multiaddr.Multiaddr, settings *plan9GuestSettings) error {
			settings.Maddr = value
			return nil
		})
}

func (o9 plan9GuestOptions) make() (plan9GuestSettings, error) {
	settings, err := makeWithOptions(o9...)
	if err != nil {
		return plan9GuestSettings{}, err
	}
	if settings.Maddr == nil {
		var (
			flagPrefix = prefixIDFlag(p9fs.GuestID)
			srvName    = flagPrefix + p9ServerFlagSuffix
		)
		return plan9GuestSettings{}, fmt.Errorf(
			"flag `-%s` must be provided for 9P guests",
			srvName,
		)
	}
	return settings, nil
}

func (s9 plan9GuestSettings) marshal(string) ([]byte, error) {
	return json.Marshal(s9)
}
