// Package fs provides an implementation of Command "fs".
package fs

import (
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/fs"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/options"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// Command returns a cmdslib (root) of Command "fs".
func Command() *cmds.Command {
	return &cmds.Command{
		Options: fs.MustMakeOptions[*fs.Settings](options.WithBuiltin(true)),
		Helptext: cmds.HelpText{
			Tagline: "File system service utility.",
		},
	}
}
