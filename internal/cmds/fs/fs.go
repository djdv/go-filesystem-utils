// Package fs provides an implementation of Command "fs".
package fs

import (
	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/fs"
	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/option"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// Command returns a new cmdslib root of Command "fs".
func Command() *cmds.Command {
	return &cmds.Command{
		Options: fs.MustMakeOptions[*fs.Settings](option.WithBuiltin(true)),
		Helptext: cmds.HelpText{
			Tagline: "File system service utility.",
		},
	}
}
