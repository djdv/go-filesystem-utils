package commands

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/multiformats/go-multiaddr"
)

type (
	plan9GuestSettings p9fs.Guest
	plan9GuestOption   func(*plan9GuestSettings) error
	plan9GuestOptions  []plan9GuestOption
)

const p9GuestSrvFlagName = "server"

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
		srvName    = flagPrefix + p9GuestSrvFlagName
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
			srvName    = flagPrefix + p9GuestSrvFlagName
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
