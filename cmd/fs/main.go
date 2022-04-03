package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/djdv/go-filesystem-utils/cmd/config"
	"github.com/djdv/go-filesystem-utils/cmd/list"
	"github.com/djdv/go-filesystem-utils/cmd/mount"
	"github.com/djdv/go-filesystem-utils/cmd/service"
	"github.com/djdv/go-filesystem-utils/cmd/unmount"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/cmdsenv"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/executor"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings/options"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs-cmds/cli"
)

func main() {
	var (
		ctx  = context.Background()
		root = &cmds.Command{
			Options: settings.MakeOptions[settings.Root](options.WithBuiltin(true)),
			Helptext: cmds.HelpText{
				Tagline: "File system service utility.",
			},
			// TODO: figure out if the Encoder gets inherited
			// and if not, which commands explicitly need it.
			Subcommands: map[string]*cmds.Command{
				config.Name:  config.Command(),
				service.Name: service.Command,
				mount.Name:   mount.Command(),
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
			cmdsenv.MakeEnvironment, executor.MakeExecutor,
		)
	)
	if err != nil {
		var cliError cli.ExitError
		if errors.As(err, &cliError) {
			os.Exit(int(cliError))
		}
	}
}
