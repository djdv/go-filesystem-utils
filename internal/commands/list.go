package commands

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/daemon"
	"github.com/djdv/go-filesystem-utils/internal/motd"
	"github.com/multiformats/go-multiaddr"
)

type listSettings struct {
	serverMaddr string
	command.HelpFlag
}

func (set *listSettings) BindFlags(fs *flag.FlagSet) {
	command.NewHelpFlag(fs, &set.HelpFlag)
	fs.StringVar(&set.serverMaddr, "maddr", daemon.ServiceMaddr, "listening socket maddr")
}

func List() command.Command {
	const (
		name     = "list"
		synopsis = "List existing files."
		usage    = "Placeholder text."
	)
	return command.MakeCommand(name, synopsis, usage, listExecute)
}

func listExecute(ctx context.Context, set *listSettings, _ ...string) error {
	serverMaddr, err := multiaddr.NewMultiaddr(set.serverMaddr)
	if err != nil {
		return err
	}

	client, err := daemon.Connect(serverMaddr)
	if err != nil {
		log.Fatal(err)
	}

	entiries, err := motd.List(client)
	if err != nil {
		log.Fatal(err)
	}

	const verbose = true // TODO: dbg lint
	fmt.Println(motd.FormatList(entiries, verbose))

	return ctx.Err()
}
