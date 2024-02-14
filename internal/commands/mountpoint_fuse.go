//go:build !nofuse

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
	"github.com/djdv/go-filesystem-utils/internal/generic"
)

type (
	fuseSettings cgofuse.Host
	fuseOption   func(*fuseSettings) error
	fuseOptions  []fuseOption
	fuseID       uint32
	fuseFlagEnv  struct {
		builtinFlags       []string
		rawOptionsProvided bool
	}
)

const (
	flagPrefixFuse         = "fuse-"
	flagNameFuseRawOptions = flagPrefixFuse + "options"
)

func makeFUSECommand() command.Command {
	return makeMountSubcommand(
		cgofuse.HostID,
		makeGuestCommands[fuseOptions, fuseSettings](cgofuse.HostID),
	)
}

func makeFUSEHost(path ninePath, autoUnlink bool) (filesystem.Host, p9fs.MakeGuestFunc) {
	guests := makeMountPointGuests[cgofuse.Host](path)
	return cgofuse.HostID, newMakeGuestFunc(guests, path, autoUnlink)
}

func unmarshalFUSE() (filesystem.Host, decodeFunc) {
	return cgofuse.HostID, func(b []byte) (string, error) {
		var host cgofuse.Host
		err := json.Unmarshal(b, &host)
		return host.Point, err
	}
}

func (*fuseOptions) usage(guest filesystem.ID) string {
	var (
		execName    = filepath.Base(os.Args[0])
		commandName = strings.TrimSuffix(
			execName,
			filepath.Ext(execName),
		)
		guestName      = strings.ToLower(string(guest))
		hostName       = strings.ToLower(string(cgofuse.HostID))
		exampleCommand = fmt.Sprintf(
			"E.g. `%s mount %s %s %s`",
			commandName, hostName, guestName, fuseExampleArgs,
		)
	)
	return "Utilizes the `cgofuse` library" +
		" to interface with the host system's FUSE API.\n\n" +
		"Flags that are common across FUSE implementations" +
		" are provided by this command,\nbut implementation" +
		" specific flags may be passed directly to the FUSE" +
		" library\nvia the `-" + flagNameFuseRawOptions + "` flag" +
		" if required.\n\n" +
		fuseHelpText +
		"\n" + exampleCommand
}

func (fo *fuseOptions) BindFlags(flagSet *flag.FlagSet) {
	var flagEnv fuseFlagEnv
	fo.bindRawFlag(flagSet, &flagEnv)
	fo.bindUIDFlag(flagSet, &flagEnv)
	fo.bindGIDFlag(flagSet, &flagEnv)
	fo.bindLogFlag(flagSet)
	fo.bindReaddirPlusFlag(flagSet)
	fo.bindCaseInsensitiveFlag(flagSet)
	fo.bindDeleteAccessFlag(flagSet)
}

func (fo *fuseOptions) bindRawFlag(flagSet *flag.FlagSet, flagEnv *fuseFlagEnv) {
	const (
		name  = flagNameFuseRawOptions
		usage = "raw options passed directly to mount" +
			"\nmust be specified once per `FUSE flag`" +
			"\n(E.g. `-" + name +
			` "-o uid=0,gid=0" -` +
			name + " \"--VolumePrefix=somePrefix\"`)"
	)
	var (
		parseFn = func(argument string) (string, error) {
			flagEnv.rawOptionsProvided = true
			if builtins := flagEnv.builtinFlags; len(builtins) != 0 {
				return "", newCombinedFlagsError(builtins)
			}
			return argument, nil
		}
		getRefFn = func(settings *fuseSettings) *[]string {
			return &settings.Options
		}
	)
	appendFlagList(flagSet, name, usage,
		fo, parseFn, getRefFn)
}

func newCombinedFlagsError(builtingFlags []string) error {
	const explicitErr = "cannot combine raw options flag `-" +
		flagNameFuseRawOptions + "` with built-in flags"
	for i, flag := range builtingFlags {
		builtingFlags[i] = "-" + flag
	}
	return fmt.Errorf("%s: %s",
		explicitErr,
		strings.Join(builtingFlags, ","),
	)
}

func (fo *fuseOptions) bindUIDFlag(flagSet *flag.FlagSet, flagEnv *fuseFlagEnv) {
	const (
		kind = "uid"
		name = flagPrefixFuse + kind
	)
	var (
		usage, defaultText = fuseIDFlagText(kind)
		parseFn            = func(argument string) (uint32, error) {
			flagEnv.builtinFlags = append(flagEnv.builtinFlags, name)
			if flagEnv.rawOptionsProvided {
				return 0, newCombinedFlagsError(flagEnv.builtinFlags)
			}
			id, err := parseID[fuseID](argument)
			return uint32(id), err
		}
		getRefFn = func(settings *fuseSettings) *uint32 {
			return &settings.UID
		}
	)
	appendFlagValue(flagSet, name, usage,
		fo, parseFn, getRefFn)
	flagSet.Lookup(name).
		DefValue = defaultText
}

func (fo *fuseOptions) bindGIDFlag(flagSet *flag.FlagSet, flagEnv *fuseFlagEnv) {
	const (
		kind = "gid"
		name = flagPrefixFuse + kind
	)
	var (
		usage, defaultText = fuseIDFlagText(kind)
		parseFn            = func(argument string) (uint32, error) {
			flagEnv.builtinFlags = append(flagEnv.builtinFlags, name)
			if flagEnv.rawOptionsProvided {
				return 0, newCombinedFlagsError(flagEnv.builtinFlags)
			}
			id, err := parseID[fuseID](argument)
			return uint32(id), err
		}
		getRefFn = func(settings *fuseSettings) *uint32 {
			return &settings.GID
		}
	)
	appendFlagValue(flagSet, name, usage,
		fo, parseFn, getRefFn)
	flagSet.Lookup(name).
		DefValue = defaultText
}

func (fo *fuseOptions) bindLogFlag(flagSet *flag.FlagSet) {
	const (
		name  = flagPrefixFuse + "log"
		usage = "sets a log `prefix` and enables logging in FUSE operations"
	)
	var (
		parseFn  = newPassthroughFunc(name)
		getRefFn = func(settings *fuseSettings) *string {
			return &settings.LogPrefix
		}
	)
	appendFlagValue(flagSet, name, usage,
		fo, parseFn, getRefFn)
}

func (fo *fuseOptions) bindReaddirPlusFlag(flagSet *flag.FlagSet) {
	const (
		name  = flagPrefixFuse + "readdir-plus"
		usage = "informs the host that the hosted file system has the readdir-plus capability"
	)
	getRefFn := func(settings *fuseSettings) *bool {
		return &settings.ReaddirPlus
	}
	appendFlagValue(flagSet, name, usage,
		fo, strconv.ParseBool, getRefFn)
	flagSet.Lookup(name).
		DefValue = strconv.FormatBool(readdirPlusCapible)
}

func (fo *fuseOptions) bindCaseInsensitiveFlag(flagSet *flag.FlagSet) {
	const (
		name  = flagPrefixFuse + "case-insensitive"
		usage = "informs the host that the hosted file system is case insensitive"
	)
	getRefFn := func(settings *fuseSettings) *bool {
		return &settings.CaseInsensitive
	}
	appendFlagValue(flagSet, name, usage,
		fo, strconv.ParseBool, getRefFn)
}

func (fo *fuseOptions) bindDeleteAccessFlag(flagSet *flag.FlagSet) {
	const (
		name  = flagPrefixFuse + "delete-access"
		usage = "``informs the host that the hosted file system implements `access` which understands the `DELETE_OK` flag"
	)
	getRefFn := func(settings *fuseSettings) *bool {
		return &settings.DeleteAccess
	}
	appendFlagValue(flagSet, name, usage,
		fo, strconv.ParseBool, getRefFn)
}

func (fo fuseOptions) make() (fuseSettings, error) {
	settings := fuseSettings{
		UID:         fuseUIDDefault,
		GID:         fuseGIDDefault,
		ReaddirPlus: readdirPlusCapible,
	}
	return settings, generic.ApplyOptions(&settings, fo...)
}

func (set fuseSettings) marshal(arg string) ([]byte, error) {
	if arg == "" &&
		set.Options == nil {
		err := command.UsageError{
			Err: generic.ConstError(
				"expected mount point",
			),
		}
		return nil, err
	}
	set.Point = arg
	return json.Marshal(set)
}
