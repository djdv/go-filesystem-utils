package fscmds

import (
	"time"

	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

const (
	// ServiceName defines a default name which server and clients may use
	// to refer to the service, in namespace oriented APIs.
	// Effectively the service root.
	ServiceName = "fs"
	// ServerName defines a default name which servers and clients may use
	// to form or find connections to the named server instance.
	// (E.g. a Unix socket of path `.../$ServiceName/$ServerName`.)
	ServerName = "server"
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
