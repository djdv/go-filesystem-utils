package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/commands/chmod"
	"github.com/djdv/go-filesystem-utils/internal/commands/client"
	"github.com/djdv/go-filesystem-utils/internal/commands/daemon"
	"github.com/djdv/go-filesystem-utils/internal/commands/mount"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/p9/p9"
)

type (
	// mountSettings holds parsed command line
	// values; it gets passed to the mount command's
	// execute function, already initialized by [flag.Parse].
	// ([flag.FlagSet] parsers are bound via [mountOptions].)
	mountSettings[
		hPtr hostPtr[host],
		host any,
	] struct {
		host  hPtr
		guest mountpoint.Guest
		mount []mount.Option
	}
	// mountOption is a generic option type used for
	// all permutations of `mount $host $guest` combinations.
	mountOption[
		hI hostPtr[hT],
		gI guestPtr[gT],
		hT, gT any,
	] func(*mountSettings[hI, hT]) error

	// mountOptions is just a list of [mountOption]
	// which implements [command.FlagBinder].
	mountOptions[
		hI hostPtr[hT],
		gI guestPtr[gT],
		hT, gT any,
	] []mountOption[hI, gI, hT, gT]
	// hostPtr types are used during the
	// construction of the [Mount] [command.Command].
	// (For generating CLI help text, and binding
	// any host specific CLI flags.)
	// It should be implemented for each host.
	hostPtr[
		T any,
	] interface {
		*T
		command.FlagBinder
		// usage must accept a `nil` receiver.
		usage(filesystem.ID) string
		// ID must accept a `nil` receiver.
		ID() filesystem.Host
		make(point string) (mountpoint.Host, error)
	}
	// guestPtr is the guest counterpart
	// of [hostPtr].
	guestPtr[T any] interface {
		*T
		command.FlagBinder
		// usage must accept a `nil` receiver.
		usage(filesystem.Host) string
		// ID must accept a `nil` receiver.
		ID() filesystem.ID
		make() (mountpoint.Guest, error)
	}
	makeHostFunc[T any] func(argument string, options ...T) (mountpoint.Host, error)
)

// Mount constructs the [command.Command]
// which requests the file system service to mount a system.
func Mount() command.Command {
	const (
		name     = "mount"
		synopsis = "Mount file systems."
	)
	if subcommands := makeSubcommands(); len(subcommands) != 0 {
		return command.SubcommandGroup(name, synopsis, subcommands)
	}
	const usage = "No mount host APIs were built into this executable."
	return command.MakeNiladicCommand(
		name, synopsis, usage,
		func(ctx context.Context) error {
			return command.UsageError{
				Err: generic.ConstError("no host systems"),
			}
		},
	)
}

func makeSubcommands() []command.Command {
	hosts := makeHostCommands()
	sortCommands(hosts)
	return hosts
}

func makeHostCommands() []command.Command {
	type makeCommand func() command.Command
	var (
		commandMakers = []makeCommand{
			makeFUSECommand,
			makeNFSHostCommand,
		}
		commands = make([]command.Command, 0, len(commandMakers))
	)
	for _, makeCommand := range commandMakers {
		// Commands can be nil if system
		// is disabled by build constraints.
		if command := makeCommand(); command != nil {
			commands = append(commands, command)
		}
	}
	return commands
}

func sortCommands(commands []command.Command) {
	slices.SortFunc(
		commands,
		func(a, b command.Command) int {
			return strings.Compare(
				a.Name(),
				b.Name(),
			)
		},
	)
}

func newMountSubcommand(host filesystem.Host, guestCommands []command.Command) command.Command {
	var (
		formalName  = string(host)
		commandName = strings.ToLower(formalName)
		synopsis    = fmt.Sprintf("Mount a file system via the %s API.", formalName)
	)
	if len(guestCommands) > 0 {
		return command.SubcommandGroup(commandName, synopsis, guestCommands)
	}
	const usage = "No mount guest APIs were built into this executable."
	return command.MakeNiladicCommand(
		commandName, synopsis, usage,
		func(ctx context.Context) error {
			return command.UsageError{
				Err: generic.ConstError("no guest systems"),
			}
		},
	)
}

func newMountCommand[
	hI hostPtr[hT],
	gI guestPtr[gT],
	hT, gT any,
]() command.Command {
	var (
		nilHost         = hI(nil)
		hostID          = nilHost.ID()
		hostFormalName  = string(hostID)
		nilGuest        = gI(nil)
		guestID         = nilGuest.ID()
		guestFormalName = string(guestID)
		cmdName         = strings.ToLower(guestFormalName)
		synopsis        = fmt.Sprintf(
			"Mount %s via the %s API.",
			guestFormalName, hostFormalName,
		)
		usage = heading(synopsis) + "\n\n" +
			underline(hostFormalName) + "\n" +
			nilHost.usage(guestID) + "\n\n" +
			underline(guestFormalName) + "\n" +
			nilGuest.usage(hostID)
	)
	type (
		optionT  = mountOption[hI, gI, hT, gT]
		optionsT = mountOptions[hI, gI, hT, gT]
	)
	return command.MakeVariadicCommand[optionsT](cmdName, synopsis, usage,
		func(ctx context.Context, arguments []string, options ...optionT) error {
			if len(arguments) == 0 {
				const err = generic.ConstError(
					"Error: no mount target was provided" +
						" in the command's arguments",
				)
				return command.UsageError{Err: err}
			}
			settings, err := optionsT(options).make()
			if err != nil {
				return err
			}
			var (
				guest       = settings.guest
				mountpoints = make([]mount.Marshaler, len(arguments))
			)
			for i, argument := range arguments {
				host, err := settings.host.make(argument)
				if err != nil {
					return err
				}
				mountpoints[i] = mountpoint.NewPair(hostID, guestID, host, guest)
			}
			return errors.Join(
				mount.Attach(hostID, guestID, mountpoints, settings.mount...),
				ctx.Err(),
			)
		})
}

func makeGuestCommands[
	hI hostPtr[hT],
	hT any,
](host filesystem.Host,
) []command.Command {
	guests := makeIPFSCommands[hI](host)
	if nfsGuest := makeNFSGuestCommand[hI](host); nfsGuest != nil {
		guests = append(guests, nfsGuest)
	}
	sortCommands(guests)
	return guests
}

func (mo *mountOptions[hI, gI, hT, gT]) BindFlags(flagSet *flag.FlagSet) {
	mo.bindClientFlags(flagSet)
	mo.bindHostFlags(flagSet)
	mo.bindGuestFlags(flagSet)
	mo.bindUIDFlag(flagSet)
	mo.bindGIDFlag(flagSet)
	mo.bindPermissionsFlag(flagSet)
}

func (mo *mountOptions[hI, gI, hT, gT]) bindClientFlags(flagSet *flag.FlagSet) {
	type optionT = mountOption[hI, gI, hT, gT]
	var (
		defaults = client.Options{
			client.AutoStartDaemon(true),
		}
		set               bool
		index             int
		withClientOptions = func(options ...client.Option) optionT {
			return func(ms *mountSettings[hI, hT]) error {
				if set {
					(ms.mount)[index] = mount.WithClientOptions(options...)
				} else {
					index = len(ms.mount)
					ms.mount = append(ms.mount, mount.WithClientOptions(options...))
					set = true
				}
				return nil
			}
		}
	)
	inheritClientFlags(
		flagSet, mo, defaults,
		withClientOptions,
	)
}

func (mo *mountOptions[hI, gI, hT, gT]) bindHostFlags(flagSet *flag.FlagSet) {
	var host hT
	hI(&host).BindFlags(flagSet)
	*mo = append(*mo, func(ms *mountSettings[hI, hT]) error {
		ms.host = hI(&host)
		return nil
	})
}

func (mo *mountOptions[hI, gI, hT, gT]) bindGuestFlags(flagSet *flag.FlagSet) {
	var settings gT
	gI(&settings).BindFlags(flagSet)
	*mo = append(*mo, func(ms *mountSettings[hI, hT]) error {
		marshaller, err := gI(&settings).make()
		if err != nil {
			return err
		}
		ms.guest = marshaller
		return nil
	})
}

func (mo *mountOptions[hI, gI, hT, gT]) bindUIDFlag(flagSet *flag.FlagSet) {
	const (
		prefix = daemon.FlagPrefix
		name   = prefix + "uid"
		usage  = "file owner's `uid`"
	)
	type optionT = mountOption[hI, gI, hT, gT]
	transformFn := func(uid p9.UID) optionT {
		return func(ms *mountSettings[hI, hT]) error {
			ms.mount = append(ms.mount, mount.WithUID(uid))
			return nil
		}
	}
	insertSliceOnce(
		flagSet, name, usage,
		mo, parseID[p9.UID], transformFn,
	)
	flagSet.Lookup(name).
		DefValue = idString(daemon.DefaultUID)
}

func (mo *mountOptions[hI, gI, hT, gT]) bindGIDFlag(flagSet *flag.FlagSet) {
	const (
		prefix = daemon.FlagPrefix
		name   = prefix + "gid"
		usage  = "file owner's `gid`"
	)
	type optionT = mountOption[hI, gI, hT, gT]
	transformFn := func(gid p9.GID) optionT {
		return func(ms *mountSettings[hI, hT]) error {
			ms.mount = append(ms.mount, mount.WithGID(gid))
			return nil
		}
	}
	insertSliceOnce(
		flagSet, name, usage,
		mo, parseID[p9.GID], transformFn,
	)
	flagSet.Lookup(name).
		DefValue = idString(daemon.DefaultGID)
}

func (mo *mountOptions[hI, gI, hT, gT]) bindPermissionsFlag(flagSet *flag.FlagSet) {
	const (
		prefix                     = daemon.FlagPrefix
		name                       = prefix + "permissions"
		usage                      = "`permissions` to use when creating service files"
		mountAPIPermissionsDefault = p9fs.ReadUser | p9fs.WriteUser | p9fs.ExecuteUser |
			p9fs.ReadGroup | p9fs.ExecuteGroup |
			p9fs.ReadOther | p9fs.ExecuteOther
	)
	type optionT = mountOption[hI, gI, hT, gT]
	var (
		apiPermissions = mountAPIPermissionsDefault
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
			// As would be done with multiple calls of
			// `chmod` when operating on a file's metadata.
			apiPermissions = p9.FileMode(permissions)
			return apiPermissions, nil
		}
		set         bool
		index       int
		transformFn = func(permissions p9.FileMode) optionT {
			return func(ms *mountSettings[hI, hT]) error {
				option := mount.WithPermissions(permissions)
				if set {
					ms.mount[index] = option
				} else {
					index = len(ms.mount)
					ms.mount = append(ms.mount, option)
					set = true
				}
				return nil
			}
		}
	)
	upsertSlice(
		flagSet, name, usage,
		mo, parseFn, transformFn,
	)
	flagSet.Lookup(name).
		DefValue = chmod.ToSymbolic(
		fs.FileMode(mountAPIPermissionsDefault),
	)
}

func (mo mountOptions[hI, gI, hT, gT]) make() (mountSettings[hI, hT], error) {
	var settings mountSettings[hI, hT]
	err := generic.ApplyOptions(&settings, mo...)
	return settings, err
}
