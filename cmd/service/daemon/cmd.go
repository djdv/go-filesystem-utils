package daemon

import (
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

const Name = "daemon"

// TODO: do we need anything?
type Settings = settings.Root

// Command returns an instance of the `daemon` command.
func Command() *cmds.Command {
	return &cmds.Command{
		Helptext: cmds.HelpText{
			Tagline: "Manages file system requests and instances.",
		},
		NoRemote: true,
		PreRun:   daemonPreRun,
		Run:      daemonRun,
		Options:  settings.MakeOptions[Settings](),
		Encoders: settings.CmdsEncoders,
		Type:     Response{},
		Subcommands: map[string]*cmds.Command{
			stop.Name: stop.Command,
		},
	}
}

// CmdsPath returns the leading parameters
// to invoke the daemon's `Run` method from `main`.
func CmdsPath() []string { return []string{"service", Name} }
