package service_test

import (
	"context"
	"errors"
	"os"
	"testing"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/service"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs-cmds/cli"
)

var testRoot = &cmds.Command{
	Options: fscmds.Options,
	Helptext: cmds.HelpText{
		Tagline: "File system service ⚠[TEST]⚠ utility.",
	},
	Subcommands: map[string]*cmds.Command{
		service.Name: service.Command,
	},
}

func TestMain(m *testing.M) {
	// When called with service arguments,
	// call the service's main function
	if len(os.Args) >= 2 && os.Args[1] == service.Name {
		var (
			ctx = context.Background()
			err = cli.Run(ctx, testRoot, os.Args,
				os.Stdin, os.Stdout, os.Stderr,
				service.MakeEnvironment, service.MakeExecutor)
		)
		if err != nil {
			cliError := new(cli.ExitError)
			if errors.As(err, cliError) {
				os.Exit(int(*cliError))
			}
		}
		os.Exit(0)
	}
	// otherwise call Go's standard testing.Main
	os.Exit(m.Run())
}
