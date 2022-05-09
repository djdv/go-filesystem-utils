package cmdslib

import (
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
	}
}
