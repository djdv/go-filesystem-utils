//go:build !nofuse

package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
)

type fuseHost cgofuse.Mounter

const (
	flagPrefixFuse         = "fuse-"
	flagNameFuseRawOptions = flagPrefixFuse + "options"
)

func makeFUSECommand() command.Command {
	return newMountSubcommand(
		cgofuse.Host,
		makeGuestCommands[*fuseHost](cgofuse.Host),
	)
}

func (*fuseHost) ID() filesystem.Host {
	return cgofuse.Host
}

func (*fuseHost) usage(guest filesystem.ID) string {
	var (
		execName    = filepath.Base(os.Args[0])
		commandName = strings.TrimSuffix(
			execName,
			filepath.Ext(execName),
		)
		guestName      = strings.ToLower(string(guest))
		hostName       = strings.ToLower(string(cgofuse.Host))
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

func (settings *fuseHost) BindFlags(flagSet *flag.FlagSet) {
	const system = cgofuse.Host
	settings.bindRawFlag(flagSet)
	settings.bindUIDFlag(flagSet)
	settings.bindGIDFlag(flagSet)
	settings.bindLogFlag(flagSet)
	settings.bindReaddirPlusFlag(flagSet)
	settings.bindCaseInsensitiveFlag(flagSet)
	settings.bindDeleteAccessFlag(flagSet)
}

func (settings *fuseHost) newSetFunc(attribute string) flagSetFunc {
	return func(argument string) error {
		return (*cgofuse.Mounter)(settings).ParseField(attribute, argument)
	}
}

func (settings *fuseHost) bindRawFlag(flagSet *flag.FlagSet) {
	const (
		name  = flagNameFuseRawOptions
		usage = "raw options passed directly to mount" +
			"\nmust be specified once per `FUSE flag`" +
			"\n(E.g. `-" + name +
			` "-o uid=0,gid=0" -` +
			name + " \"--VolumePrefix=somePrefix\"`)"
	)
	setFlag[[]string](
		flagSet, name, usage,
		func(argument string) error {
			settings.Options = append(
				settings.Options, argument,
			)
			return nil
		})
}

func (settings *fuseHost) bindUIDFlag(flagSet *flag.FlagSet) {
	const (
		kind = "uid"
		name = flagPrefixFuse + kind
	)
	usage, defaultText := fuseIDFlagText(kind)
	setFlagOnce[uint32](
		flagSet, name, usage,
		settings.newSetFunc(cgofuse.UIDAttribute),
	)
	flagSet.Lookup(name).
		DefValue = defaultText
}

func (settings *fuseHost) bindGIDFlag(flagSet *flag.FlagSet) {
	const (
		kind = "gid"
		name = flagPrefixFuse + kind
	)
	usage, defaultText := fuseIDFlagText(kind)
	setFlagOnce[uint32](
		flagSet, name, usage,
		settings.newSetFunc(cgofuse.GIDAttribute),
	)
	flagSet.Lookup(name).
		DefValue = defaultText
}

func (settings *fuseHost) bindLogFlag(flagSet *flag.FlagSet) {
	const (
		name  = flagPrefixFuse + "log"
		usage = "sets a log `prefix` and enables logging in FUSE operations"
	)
	setFlagOnce[string](
		flagSet, name, usage,
		settings.newSetFunc(cgofuse.LogPrefixAttribute),
	)
}

func (settings *fuseHost) bindReaddirPlusFlag(flagSet *flag.FlagSet) {
	const (
		name  = flagPrefixFuse + "readdir-plus"
		usage = "informs the host that the hosted file system has the readdir-plus capability"
	)
	setFlagOnce[bool](
		flagSet, name, usage,
		settings.newSetFunc(cgofuse.ReaddirPlusAttribute),
	)
}

func (settings *fuseHost) bindCaseInsensitiveFlag(flagSet *flag.FlagSet) {
	const (
		name  = flagPrefixFuse + "case-insensitive"
		usage = "informs the host that the hosted file system is case insensitive"
	)
	setFlagOnce[bool](
		flagSet, name, usage,
		settings.newSetFunc(cgofuse.CaseInsensitiveAttribute),
	)
}

func (settings *fuseHost) bindDeleteAccessFlag(flagSet *flag.FlagSet) {
	const (
		name  = flagPrefixFuse + "delete-access"
		usage = "provides a `path` that will be denied" +
			" `DELETE_OK` privileges when `access` is called" +
			"\nthis flag can be supplied multiple times"
	)
	setFlag[[]string](
		flagSet, name, usage,
		func(argument string) error {
			settings.DenyDeletePaths = append(
				settings.DenyDeletePaths, argument,
			)
			return nil
		})
}

func (settings *fuseHost) make(point string) (mountpoint.Host, error) {
	clone := (cgofuse.Mounter)(*settings)
	clone.Point = point
	return &clone, nil
}
