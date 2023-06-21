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
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
	"github.com/djdv/go-filesystem-utils/internal/generic"
)

type (
	fuseSettings cgofuse.Host
	fuseOption   func(*fuseSettings) error
	fuseOptions  []fuseOption
	fuseID       uint32
)

const (
	fuseFlagPrefix     = "fuse-"
	fuseRawOptionsName = fuseFlagPrefix + "options"
)

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
		" library\nvia the `-" + fuseRawOptionsName + "` flag" +
		" if required.\n\n" +
		fuseHelpText +
		"\n" + exampleCommand
}

func (fo *fuseOptions) BindFlags(flagSet *flag.FlagSet) {
	const (
		prefix       = fuseFlagPrefix
		optionsName  = fuseRawOptionsName
		optionsUsage = "raw options passed directly to mount" +
			"\nmust be specified once per `FUSE flag`" +
			"\n(E.g. `-" + optionsName +
			` "-o uid=0,gid=0" -` +
			optionsName + " \"--VolumePrefix=somePrefix\"`)"
		passthroughErr = "cannot combine" +
			"-" + optionsName + "with built-in flags"
	)
	var (
		passthroughFlags []string
		explicitFlags    []string
	)
	flagSetFunc(flagSet, optionsName, optionsUsage, fo,
		func(value string, settings *fuseSettings) error {
			if len(explicitFlags) != 0 {
				return fmt.Errorf("%s: %s",
					passthroughErr,
					strings.Join(explicitFlags, ","),
				)
			}
			passthroughFlags = append(passthroughFlags, value)
			settings.Options = append(settings.Options, value)
			return nil
		})
	const (
		uidKind     = "uid"
		uidName     = prefix + uidKind
		gidKind     = "gid"
		gidName     = prefix + gidKind
		explicitErr = "cannot combine built-in flags" +
			"with -" + optionsName
	)
	var (
		uidUsage, uidDefaultText = fuseIDFlagText(uidKind)
		gidUsage, gidDefaultText = fuseIDFlagText(gidKind)
		combinedCheck            = func() error {
			if len(passthroughFlags) != 0 {
				return fmt.Errorf("%s: %s",
					explicitErr,
					strings.Join(passthroughFlags, ","),
				)
			}
			return nil
		}
	)
	flagSetFunc(flagSet, uidName, uidUsage, fo,
		func(value fuseID, settings *fuseSettings) error {
			if err := combinedCheck(); err != nil {
				return err
			}
			explicitFlags = append(explicitFlags, uidName)
			settings.UID = uint32(value)
			return nil
		})
	flagSet.Lookup(uidName).
		DefValue = uidDefaultText
	flagSetFunc(flagSet, gidName, gidUsage, fo,
		func(value fuseID, settings *fuseSettings) error {
			if err := combinedCheck(); err != nil {
				return err
			}
			explicitFlags = append(explicitFlags, gidName)
			settings.GID = uint32(value)
			return nil
		})
	flagSet.Lookup(gidName).
		DefValue = gidDefaultText
	const (
		logName  = prefix + "log"
		logUsage = "sets a log `prefix` and enables logging in FUSE operations"
	)
	flagSetFunc(flagSet, logName, logUsage, fo,
		func(value string, settings *fuseSettings) error {
			if value == "" {
				return fmt.Errorf(`"%s" flag had empty value`, logName)
			}
			settings.LogPrefix = value
			return nil
		})
	const (
		readdirName  = prefix + "readdir-plus"
		readdirUsage = "informs the host that the hosted file system has the readdir-plus capability"
	)
	flagSetFunc(flagSet, readdirName, readdirUsage, fo,
		func(value bool, settings *fuseSettings) error {
			settings.ReaddirPlus = value
			return nil
		})

	flagSet.Lookup(readdirName).
		DefValue = strconv.FormatBool(readdirPlusCapible)
	const (
		caseName  = prefix + "case-insensitive"
		caseUsage = "informs the host that the hosted file system is case insensitive"
	)
	flagSetFunc(flagSet, caseName, caseUsage, fo,
		func(value bool, settings *fuseSettings) error {
			settings.CaseInsensitive = value
			return nil
		})
	const (
		deleteName  = prefix + "delete-access"
		deleteUsage = "informs the host that the hosted file system implements \"Access\" which understands the \"DELETE_OK\" flag"
	)
	flagSetFunc(flagSet, deleteName, deleteUsage, fo,
		func(value bool, settings *fuseSettings) error {
			settings.DeleteAccess = value
			return nil
		})
}

func (fo fuseOptions) make() (fuseSettings, error) {
	settings := fuseSettings{
		UID:         fuseUIDDefault,
		GID:         fuseGIDDefault,
		ReaddirPlus: readdirPlusCapible,
	}
	return settings, applyOptions(&settings, fo...)
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
