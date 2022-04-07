package cmdslib

import (
	"github.com/djdv/go-filesystem-utils/cmd/list"
	"github.com/djdv/go-filesystem-utils/cmd/mount"
	"github.com/djdv/go-filesystem-utils/cmd/service"
	"github.com/djdv/go-filesystem-utils/cmd/unmount"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/options"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

func Root() *cmds.Command {
	return &cmds.Command{
		Options: settings.MakeOptions[settings.Root](options.WithBuiltin(true)),
		Helptext: cmds.HelpText{
			Tagline: "File system service utility.",
		},
		// TODO: figure out if the Encoder gets inherited
		// and if not, which commands explicitly need it.
		Subcommands: map[string]*cmds.Command{
			service.Name: service.Command(),
			mount.Name:   mount.Command(),
			list.Name:    list.Command(),
			unmount.Name: unmount.Command(),
		},
	}
}
