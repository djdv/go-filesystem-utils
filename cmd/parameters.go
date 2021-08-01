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

type Settings struct {
	ServiceMaddrs []multiaddr.Multiaddr `settings:"arguments"`
	// AutoExit will cause processes spawned by this command
	// to exit (if not busy) after some time.
	AutoExit time.Duration
}

func (*Settings) Parameters() parameters.Parameters { return Parameters() }

func Parameters() parameters.Parameters {
	return parameters.Parameters{
		ServiceMaddr(),
		StopAfter(),
	}
}

func ServiceMaddr() parameters.Parameter {
	return parameters.NewParameter(
		"File system service multiaddr to use.",
		parameters.WithRootNamespace(),
		parameters.WithName("api"),
	)
}

func StopAfter() parameters.Parameter {
	return parameters.NewParameter(
		`Time interval (e.g. "30s") to check if the service is active and exit if not.`,
		parameters.WithRootNamespace(),
	)
}
