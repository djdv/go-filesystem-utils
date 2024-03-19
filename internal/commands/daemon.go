package commands

import (
	"flag"
	"io/fs"
	"strconv"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/commands/chmod"
	"github.com/djdv/go-filesystem-utils/internal/commands/daemon"
	"github.com/djdv/p9/p9"
	"github.com/multiformats/go-multiaddr"
)

type daemonOptions []daemon.Option

// Daemon constructs the [command.Command]
// which hosts a file system service server.
func Daemon() command.Command {
	const (
		name     = daemon.ServerCommandName
		synopsis = "Host system services."
	)
	usage := heading("File system service daemon.") +
		"\n\n" + synopsis
	return command.MakeVariadicCommand[daemonOptions](name, synopsis, usage, daemon.Run)
}

func (do *daemonOptions) BindFlags(flagSet *flag.FlagSet) {
	do.bindVerboseFlag(flagSet)
	do.bindSystemLogFlag(flagSet)
	do.bindProtocolLogFlag(flagSet)
	do.bindServerFlag(flagSet)
	do.bindExitFlag(flagSet)
	do.bindUIDFlag(flagSet)
	do.bindGIDFlag(flagSet)
	do.bindPermissionsFlag(flagSet)
}

func (do *daemonOptions) bindVerboseFlag(flagSet *flag.FlagSet) {
	const (
		name  = "verbose"
		usage = "enable all message loggers"
	)
	transformFn := func(verbose bool) daemon.Option {
		return daemon.WithVerbosity(verbose)
	}
	insertSliceOnce(
		flagSet, name, usage,
		do, strconv.ParseBool, transformFn,
	)
}

func (do *daemonOptions) bindSystemLogFlag(flagSet *flag.FlagSet) {
	const (
		name  = "log-daemon"
		usage = "enable the daemon's message logger"
	)
	transformFn := func(verbose bool) daemon.Option {
		return daemon.WithSystemLog(verbose)
	}
	insertSliceOnce(
		flagSet, name, usage,
		do, strconv.ParseBool, transformFn,
	)
}

func (do *daemonOptions) bindProtocolLogFlag(flagSet *flag.FlagSet) {
	const (
		name  = "log-protocol"
		usage = "enable the 9P message logger"
	)
	transformFn := func(verbose bool) daemon.Option {
		return daemon.WithProtocolLog(verbose)
	}
	insertSliceOnce(
		flagSet, name, usage,
		do, strconv.ParseBool, transformFn,
	)
}

func (do *daemonOptions) bindServerFlag(flagSet *flag.FlagSet) {
	const (
		name  = daemon.FlagServer
		usage = "listening socket `maddr`" +
			"\ncan be specified multiple times"
	)
	var (
		accumulator []multiaddr.Multiaddr
		transformFn = func(maddr multiaddr.Multiaddr) daemon.Option {
			accumulator = append(accumulator, maddr)
			return daemon.WithMaddrs(accumulator...)
		}
	)
	upsertSlice(
		flagSet, name, usage,
		do, multiaddr.NewMultiaddr, transformFn,
	)
	flagSet.Lookup(daemon.FlagServer).
		DefValue = daemon.DefaultAPIMaddr().String()
}

func (do *daemonOptions) bindExitFlag(flagSet *flag.FlagSet) {
	const (
		name  = daemon.FlagExitAfter
		usage = "check every `interval` (e.g. \"30s\") and shutdown the daemon if its idle"
	)
	transformFn := func(interval time.Duration) daemon.Option {
		return daemon.WithExitInterval(interval)
	}
	insertSliceOnce(
		flagSet, name, usage,
		do, time.ParseDuration, transformFn,
	)
}

func (do *daemonOptions) bindUIDFlag(flagSet *flag.FlagSet) {
	const (
		name  = daemon.FlagPrefix + "uid"
		usage = "file owner's `uid`"
	)
	transformFn := func(uid p9.UID) daemon.Option {
		return daemon.WithUID(uid)
	}
	insertSliceOnce(
		flagSet, name, usage,
		do, parseID, transformFn,
	)
	flagSet.Lookup(name).
		DefValue = idString(daemon.DefaultUID)
}

func (do *daemonOptions) bindGIDFlag(flagSet *flag.FlagSet) {
	const (
		name  = daemon.FlagPrefix + "gid"
		usage = "file owner's `gid`"
	)
	transformFn := func(gid p9.GID) daemon.Option {
		return daemon.WithGID(gid)
	}
	insertSliceOnce(
		flagSet, name, usage,
		do, parseID, transformFn,
	)
	flagSet.Lookup(name).
		DefValue = idString(daemon.DefaultGID)
}

func (do *daemonOptions) bindPermissionsFlag(flagSet *flag.FlagSet) {
	const (
		name  = daemon.FlagPrefix + "permissions"
		usage = "`permissions` to use when creating service files"
	)
	var (
		apiPermissions = daemon.DefaultPermissions
		parseFn        = func(argument string) (p9.FileMode, error) {
			permissions, err := chmod.ParsePermissions(
				fs.FileMode(apiPermissions),
				argument,
			)
			if err != nil {
				return 0, err
			}
			// Retain modifications of symbolic expressions
			// between multiple flags instances / calls.
			// As would be done with multiple expressions / calls
			// to `chmod` when operating on a file's metadata.
			apiPermissions = p9.FileMode(permissions)
			return apiPermissions, nil
		}
		transformFn = func(permissions p9.FileMode) daemon.Option {
			return daemon.WithPermissions(permissions)
		}
	)
	upsertSlice(
		flagSet, name, usage,
		do, parseFn, transformFn,
	)
	flagSet.Lookup(name).
		DefValue = chmod.ToSymbolic(
		fs.FileMode(daemon.DefaultPermissions),
	)
}
