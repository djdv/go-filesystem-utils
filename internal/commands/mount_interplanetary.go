//go:build !noipfs

package commands

import (
	"flag"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/commands/chmod"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	giconfig "github.com/ipfs/kubo/config" // Global IPFS config.
	"github.com/multiformats/go-multiaddr"
)

func makeIPFSCommands[
	hI hostPtr[hT],
	hT any,
](filesystem.Host,
) []command.Command {
	// NOTE: these (likely) get sorted by the caller
	// but try to keep them pre-sorted; alphabetical
	// (by command's name).
	return []command.Command{
		newMountCommand[hI, *ipfsSettings](),
		newMountCommand[hI, *ipnsSettings](),
		newMountCommand[hI, *keyfsSettings](),
		newMountCommand[hI, *pinfsSettings](),
	}
}

func guestOverlayText(overlay, overlaid filesystem.ID) string {
	return string(overlay) + " is an " + string(overlaid) + " overlay"
}

func ipfsBindAPIMaddrFlag(
	flagSet *flag.FlagSet, system filesystem.ID, prefix string,
	setFn flagSetFunc,
) {
	const (
		ipfsConfigAPIFileName = "api"
		ipfsConfigEnv         = giconfig.EnvDir
		ipfsConfigDir         = giconfig.DefaultPathRoot
	)
	var (
		usage = string(system) + " API `maddr`"
		name  = prefix + "api"
	)
	setFlagOnce[multiaddr.Multiaddr](
		flagSet, name, usage,
		setFn,
	)
	flagSet.Lookup(name).
		DefValue = fmt.Sprintf(
		"parses: %s, %s",
		filepath.Join("$"+ipfsConfigEnv, ipfsConfigAPIFileName),
		filepath.Join(ipfsConfigDir, ipfsConfigAPIFileName),
	)
}

func ipfsBindAPITimeoutFlag(
	flagSet *flag.FlagSet, prefix string,
	dfault time.Duration, setFn flagSetFunc,
) {
	const usage = "timeout `duration` to use when communicating" +
		" with the API" +
		"\nif <= 0, operations will remain pending" +
		" until the operation completes, or the file or system is closed"
	name := prefix + "timeout"
	setFlagOnce[time.Duration](
		flagSet, name, usage,
		setFn,
	)
	flagSet.Lookup(name).
		DefValue = dfault.String()
}

func ipfsBindPermissionsFlag(
	flagSet *flag.FlagSet, prefix, whatFor string,
	dfault fs.FileMode, setFn flagSetFunc,
) {
	var (
		name  = prefix + "permissions"
		usage = "`permissions` to use for " +
			whatFor + "\n(may be in octal or symbolic format)"
	)
	setFlagOnce[fs.FileMode](
		flagSet, name, usage,
		setFn,
	)
	flagSet.Lookup(name).
		DefValue = chmod.ToSymbolic(dfault)
}

func ipfsBindLinkLimitFlag(
	flagSet *flag.FlagSet, prefix string,
	dfault uint, setFn flagSetFunc,
) {
	const usage = "the maximum amount of times an operation will resolve a" +
		" symbolic link chain before it returns a recursion error"
	name := prefix + "link-limit"
	setFlagOnce[uint](
		flagSet, name, usage,
		setFn,
	)
	flagSet.Lookup(name).
		DefValue = strconv.Itoa(int(dfault))
}
