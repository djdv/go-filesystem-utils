package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/service"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs-cmds/cli"
)

func main() {
	var (
		ctx  = context.Background()
		root = &cmds.Command{
			Options: fscmds.RootOptions(),
			Helptext: cmds.HelpText{
				Tagline: "File system service utility.",
			},
			Subcommands: map[string]*cmds.Command{
				service.Name: service.Command,
			},
		}
		// cmdline[0] is used literally in helptext generation.
		ourName = filepath.Base(os.Args[0]) // We set it to the program's name.
		cmdline = append([]string{          // (sans path, extension, etc.)
			strings.TrimSuffix(ourName, filepath.Ext(ourName)),
		},
			os.Args[1:]...,
		)
		err = cli.Run(ctx, root, cmdline,
			os.Stdin, os.Stdout, os.Stderr,
			service.MakeEnvironment, service.MakeExecutor,
			)
	)
	if err != nil {
		var cliError cli.ExitError
		if errors.As(err, &cliError) {
			os.Exit(int(cliError))
		}
	}
}
