package filesystem

import (
	"time"

	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/filesystem"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

// RootOptions returns top-level cmds-lib options,
// as well as options which pertain to the `Settings` struct for this pkg.
func RootOptions() []cmds.Option {
	return append(
		parameters.CmdsOptionsFrom((*Settings)(nil)),
		[]cmds.Option{
			cmds.OptionEncodingType,
			cmds.OptionTimeout,
			cmds.OptionStreamChannels,
			cmds.BoolOption(cmds.OptLongHelp, "Show the full command help text."),
			cmds.BoolOption(cmds.OptShortHelp, "Show a short version of the command help text."),
		}...)
}

// Settings implements the `parameters.Settings` interface
// to generate parameter getters and setters, as well as help-text
// which describes their influence.
type Settings struct {
	// ServiceMaddrs is a list of addresses to server instances.
	// Commands are free to use this value for any purpose as a context hint.
	// E.g. It may be used to connect to existing servers,
	// spawn new ones, check the status of them, etc.
	ServiceMaddrs []multiaddr.Multiaddr `settings:"arguments"`
	// AutoExitInterval will cause processes spawned by this command
	// to exit (if not busy) after some interval.
	// If the process remains busy, it will remain running
	// until another stop condition is met.
	AutoExitInterval time.Duration
}

// Parameters returns the list of parameters associated with this pkg.
func (*Settings) Parameters() parameters.Parameters {
	return parameters.Parameters{
		ServiceMaddrs(),
		AutoExitInterval(),
	}
}

func ServiceMaddrs() parameters.Parameter {
	return parameters.NewParameter(
		"File system service multiaddr to use.",
		parameters.WithRootNamespace(),
		parameters.WithName("api"),
	)
}

func AutoExitInterval() parameters.Parameter {
	return parameters.NewParameter(
		`Time interval (e.g. "30s") to check if the service is active and exit if not.`,
		parameters.WithRootNamespace(),
	)
}

type HostService struct {
	Username string `settings:"arguments"`
	PlatformSettings
}

func (*HostService) Parameters() parameters.Parameters {
	var (
		pkg = []parameters.Parameter{
			Username(),
		}
		system = (*PlatformSettings)(nil).Parameters()
	)
	return append(pkg, system...)
}

func Username() parameters.Parameter {
	return parameters.NewParameter(
		"Username to use when interfacing with the system service manager.",
	)
}

type MountSettings struct {
	HostAPI   filesystem.API `settings:"arguments"`
	FSID      filesystem.ID
	IPFSMaddr multiaddr.Multiaddr
}

func (*MountSettings) Parameters() parameters.Parameters {
	return []parameters.Parameter{
		SystemAPI(),
		SystemID(),
		IPFS(),
	}
}

func SystemAPI() parameters.Parameter {
	return parameters.NewParameter(
		"Host system API to use.",
		parameters.WithName("system"),
	)
}

func SystemID() parameters.Parameter {
	return parameters.NewParameter(
		"Target FS to use.",
		parameters.WithName("fs"),
	)
}

func IPFS() parameters.Parameter {
	return parameters.NewParameter(
		"IPFS multiaddr to use.",
	)
}

type UnmountSettings struct {
	MountSettings
	All bool
}

func (*UnmountSettings) Parameters() parameters.Parameters {
	return append((*MountSettings)(nil).Parameters(),
		All(),
	)
}

func All() parameters.Parameter {
	return parameters.NewParameter(
		"Unmount all mountpoints.",
		parameters.WithNameAlias("a"),
	)
}
