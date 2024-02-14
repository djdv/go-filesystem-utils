package commands

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/p9/p9"
	"github.com/jaevor/go-nanoid"
)

type (
	marshaller interface {
		marshal(argument string) ([]byte, error)
	}
	mountCmdConstraint[T any, M marshaller] interface {
		*T
		command.FlagBinder
		make() (M, error)
	}
	mountCmdHost[T any, M marshaller] interface {
		mountCmdConstraint[T, M]
		usage(filesystem.ID) string
	}
	mountCmdGuest[
		T any,
		M marshaller,
	] interface {
		mountCmdConstraint[T, M]
		usage(filesystem.Host) string
	}
	mountSettings struct {
		permissions p9.FileMode
		uid         p9.UID
		gid         p9.GID
	}
	mountCmdSettings[
		HM, GM marshaller,
	] struct {
		clientSettings
		host       HM
		guest      GM
		apiOptions []MountOption
	}
	mountCmdOption[
		// Host/Guest marshaller constructor types.
		// (Typically a slice of functional options.)
		HT, GT any,
		// Result type of the constructors.
		// (Typically a struct with options applied.)
		HM, GM marshaller,
		// Constraints on *{H,G}T.
		// (Needs to satisfy requirements of the `mount` command.)
		HC mountCmdHost[HT, HM],
		GC mountCmdGuest[GT, GM],
	] func(*mountCmdSettings[HM, GM]) error
	mountCmdOptions[
		HT, GT any,
		HM, GM marshaller,
		HC mountCmdHost[HT, HM],
		GC mountCmdGuest[GT, GM],
	] []mountCmdOption[HT, GT, HM, GM, HC, GC]
	MountOption func(*mountSettings) error
)

const (
	mountAPIPermissionsDefault = p9fs.ReadUser | p9fs.WriteUser | p9fs.ExecuteUser |
		p9fs.ReadGroup | p9fs.ExecuteGroup |
		p9fs.ReadOther | p9fs.ExecuteOther
)

func WithPermissions(permissions p9.FileMode) MountOption {
	return func(ms *mountSettings) error {
		ms.permissions = permissions
		return nil
	}
}

func WithUID(uid p9.UID) MountOption {
	return func(ms *mountSettings) error {
		ms.uid = uid
		return nil
	}
}

func WithGID(gid p9.GID) MountOption {
	return func(ms *mountSettings) error {
		ms.gid = gid
		return nil
	}
}

func (mo *mountCmdOptions[HT, GT, HM, GM, HC, GC]) BindFlags(flagSet *flag.FlagSet) {
	type cmdSettings = mountCmdSettings[HM, GM]
	mo.bindClientFlags(flagSet)
	mo.bindHostFlags(flagSet)
	mo.bindGuestFlags(flagSet)
	mo.bindUIDFlag(flagSet)
	mo.bindGIDFlag(flagSet)
	mo.bindPermissionsFlag(flagSet)
}

func (mo *mountCmdOptions[HT, GT, HM, GM, HC, GC]) bindClientFlags(flagSet *flag.FlagSet) {
	type cmdSettings = mountCmdSettings[HM, GM]
	extendFlagSet[*clientOptions](mo, flagSet,
		func(settings *cmdSettings) *clientSettings {
			return &settings.clientSettings
		})
}

func (mo *mountCmdOptions[HT, GT, HM, GM, HC, GC]) bindHostFlags(flagSet *flag.FlagSet) {
	type cmdSettings = mountCmdSettings[HM, GM]
	var host HC = new(HT)
	host.BindFlags(flagSet)
	*mo = append(*mo, func(ms *cmdSettings) error {
		marshaller, err := host.make()
		if err != nil {
			return err
		}
		ms.host = marshaller
		return nil
	})
}

func (mo *mountCmdOptions[HT, GT, HM, GM, HC, GC]) bindGuestFlags(flagSet *flag.FlagSet) {
	type cmdSettings = mountCmdSettings[HM, GM]
	var guest GC = new(GT)
	guest.BindFlags(flagSet)
	*mo = append(*mo, func(ms *cmdSettings) error {
		marshaller, err := guest.make()
		if err != nil {
			return err
		}
		ms.guest = marshaller
		return nil
	})
}

func (mo *mountCmdOptions[HT, GT, HM, GM, HC, GC]) bindUIDFlag(flagSet *flag.FlagSet) {
	type cmdSettings = mountCmdSettings[HM, GM]
	const (
		prefix = "api-"
		name   = prefix + "uid"
		usage  = "file owner's `uid`"
	)
	assignFn := func(settings *cmdSettings, uid p9.UID) error {
		settings.apiOptions = append(
			settings.apiOptions,
			WithUID(uid),
		)
		return nil
	}
	appendFlagOption(flagSet, name, usage,
		mo, parseID[p9.UID], assignFn)
	flagSet.Lookup(name).
		DefValue = idString(defaultAPIUID)
}

func (mo *mountCmdOptions[HT, GT, HM, GM, HC, GC]) bindGIDFlag(flagSet *flag.FlagSet) {
	type cmdSettings = mountCmdSettings[HM, GM]
	const (
		prefix = "api-"
		name   = prefix + "gid"
		usage  = "file owner's `gid`"
	)
	assignFn := func(settings *cmdSettings, gid p9.GID) error {
		settings.apiOptions = append(
			settings.apiOptions,
			WithGID(gid),
		)
		return nil
	}
	appendFlagOption(flagSet, name, usage,
		mo, parseID[p9.GID], assignFn)
	flagSet.Lookup(name).
		DefValue = idString(defaultAPIGID)
}

func (mo *mountCmdOptions[HT, GT, HM, GM, HC, GC]) bindPermissionsFlag(flagSet *flag.FlagSet) {
	type cmdSettings = mountCmdSettings[HM, GM]
	const (
		prefix = "api-"
		name   = prefix + "permissions"
		usage  = "`permissions` to use when creating service files"
	)
	var (
		apiPermissions = fs.FileMode(mountAPIPermissionsDefault)
		parseFn        = func(argument string) (p9.FileMode, error) {
			permissions, err := parsePOSIXPermissions(apiPermissions, argument)
			if err != nil {
				return 0, err
			}
			// Retain modifications of symbolic expressions
			// between multiple flags instances / calls.
			// As would be done with multiple calls of
			// `chmod` when operating on a file's metadata.
			apiPermissions = permissions
			return modeFromFS(apiPermissions), nil
		}
		assignFn = func(settings *cmdSettings, permissions p9.FileMode) error {
			settings.apiOptions = append(
				settings.apiOptions,
				WithPermissions(permissions),
			)
			return nil
		}
	)
	defaultText := modeToSymbolicPermissions(
		fs.FileMode(mountAPIPermissionsDefault),
	)
	appendFlagOption(flagSet, name, usage,
		mo, parseFn, assignFn)
	flagSet.Lookup(name).
		DefValue = defaultText
}

func (mo mountCmdOptions[HT, GT, HM, GM, HC, GC]) make() (mountCmdSettings[HM, GM], error) {
	return makeWithOptions(mo...)
}

func (mp *mountCmdSettings[HM, GM]) marshalMountpoints(args ...string) ([][]byte, error) {
	if len(args) == 0 {
		args = []string{""}
	}
	data := make([][]byte, len(args))
	for i, arg := range args {
		hostData, err := mp.host.marshal(arg)
		if err != nil {
			return nil, err
		}
		guestData, err := mp.guest.marshal(arg)
		if err != nil {
			return nil, err
		}
		datum, err := json.Marshal(struct {
			Host  json.RawMessage `json:"host,omitempty"`
			Guest json.RawMessage `json:"guest,omitempty"`
		}{
			Host:  hostData,
			Guest: guestData,
		})
		if err != nil {
			return nil, err
		}
		data[i] = datum
	}
	return data, nil
}

// Mount constructs the command which requests
// the file system service to mount a system.
func Mount() command.Command {
	const (
		name     = "mount"
		synopsis = "Mount file systems."
	)
	if subcommands := makeMountSubcommands(); len(subcommands) != 0 {
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

func makeMountSubcommands() []command.Command {
	hosts := makeHostCommands()
	sortCommands(hosts)
	return hosts
}

func makeMountSubcommand(host filesystem.Host, guestCommands []command.Command) command.Command {
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

func makeHostCommands() []command.Command {
	type makeCommand func() command.Command
	var (
		commandMakers = []makeCommand{
			makeFUSECommand,
			makeNFSCommand,
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

func makeGuestCommands[
	HT any,
	HM marshaller,
	HC mountCmdHost[HT, HM],
](host filesystem.Host,
) []command.Command {
	guests := makeIPFSCommands[HC, HM](host)
	if nfsGuest := makeNFSGuestCommand[HC, HM](host); nfsGuest != nil {
		guests = append(guests, nfsGuest)
	}
	sortCommands(guests)
	return guests
}

func makeMountCommand[
	HC mountCmdHost[HT, HM],
	HM marshaller,
	GT any,
	GM marshaller,
	GC mountCmdGuest[GT, GM],
	HT any,
](host filesystem.Host, guest filesystem.ID,
) command.Command {
	type (
		MO  = mountCmdOption[HT, GT, HM, GM, HC, GC]
		MOS = mountCmdOptions[HT, GT, HM, GM, HC, GC]
	)
	var (
		hostFormalName  = string(host)
		guestFormalName = string(guest)
		cmdName         = strings.ToLower(guestFormalName)
		synopsis        = fmt.Sprintf(
			"Mount %s via the %s API.",
			guestFormalName, hostFormalName,
		)
		usage = header(synopsis) + "\n\n" +
			underline(hostFormalName) + "\n" +
			HC(nil).usage(guest) + "\n\n" +
			underline(guestFormalName) + "\n" +
			GC(nil).usage(host)
	)
	return command.MakeVariadicCommand[MOS](cmdName, synopsis, usage,
		func(ctx context.Context, arguments []string, options ...MO) error {
			settings, err := MOS(options).make()
			if err != nil {
				return err
			}
			data, err := settings.marshalMountpoints(arguments...)
			if err != nil {
				return err
			}
			const autoLaunchDaemon = true
			client, err := settings.getClient(autoLaunchDaemon)
			if err != nil {
				return err
			}
			apiOptions := settings.apiOptions
			if err := client.Mount(host, guest, data, apiOptions...); err != nil {
				return errors.Join(err, client.Close())
			}
			if err := client.Close(); err != nil {
				return err
			}
			return ctx.Err()
		})
}

func (c *Client) Mount(host filesystem.Host, fsid filesystem.ID, data [][]byte, options ...MountOption) error {
	set := mountSettings{
		permissions: mountAPIPermissionsDefault,
		uid:         defaultAPIUID,
		gid:         defaultAPIGID,
	}
	if err := generic.ApplyOptions(&set, options...); err != nil {
		return err
	}
	mounts, err := (*p9.Client)(c).Attach(mountsFileName)
	if err != nil {
		return err
	}
	var (
		hostName    = string(host)
		fsName      = string(fsid)
		wnames      = []string{hostName, fsName}
		permissions = set.permissions
		uid         = set.uid
		gid         = set.gid
	)
	guests, err := p9fs.MkdirAll(mounts, wnames, permissions, uid, gid)
	if err != nil {
		err = receiveError(mounts, err)
		return errors.Join(err, mounts.Close())
	}
	const (
		mountIDLength  = 9
		base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	)
	idGen, err := nanoid.CustomASCII(base58Alphabet, mountIDLength)
	if err != nil {
		return errors.Join(err, mounts.Close(), guests.Close())
	}
	var (
		errs            []error
		filePermissions = permissions ^ (p9fs.ExecuteOther | p9fs.ExecuteGroup | p9fs.ExecuteUser)
	)
	for _, data := range data {
		name := fmt.Sprintf("%s.json", idGen())
		if err := newMountFile(guests, filePermissions, uid, gid,
			name, data); err != nil {
			errs = append(errs, err)
		}
	}
	if errs != nil {
		err = errors.Join(errs...)
	}
	err = errors.Join(err, guests.Close())
	if err != nil {
		err = receiveError(mounts, err)
	}
	return errors.Join(err, mounts.Close())
}

func newMountFile(idRoot p9.File,
	permissions p9.FileMode, uid p9.UID, gid p9.GID,
	name string, data []byte,
) error {
	_, idClone, err := idRoot.Walk(nil)
	if err != nil {
		return err
	}
	targetFile, _, _, err := idClone.Create(name, p9.WriteOnly, permissions, uid, gid)
	if err != nil {
		return errors.Join(err, idClone.Close())
	}
	// NOTE: targetFile and idClone are now aliased
	// (same fid because of `Create`).
	if _, err := targetFile.WriteAt(data, 0); err != nil {
		return errors.Join(err, targetFile.Close())
	}
	return targetFile.Close()
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
