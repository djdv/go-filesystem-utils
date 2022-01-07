package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/djdv/go-filesystem-utils/cmd/environment"
	"github.com/djdv/go-filesystem-utils/cmd/executor"
	fscmds "github.com/djdv/go-filesystem-utils/filesystem/cmds"
	"github.com/djdv/go-filesystem-utils/cmd/list"
	"github.com/djdv/go-filesystem-utils/cmd/mount"
	"github.com/djdv/go-filesystem-utils/cmd/service"
	"github.com/djdv/go-filesystem-utils/cmd/unmount"
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
				mount.Name:   mount.Command,
				list.Name:    list.Command,
				unmount.Name: unmount.Command,
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
			environment.MakeEnvironment, executor.MakeExecutor,
		)
	)
	if err != nil {
		var cliError cli.ExitError
		if errors.As(err, &cliError) {
			os.Exit(int(cliError))
		}
	}
}
