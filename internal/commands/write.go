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

// TODO: dedupe settings, they share a common base
// TODO: filename should be flag not argument
// ^ defaults to username
type writeSettings struct {
	serverMaddr string
	command.HelpFlag
}

func (set *writeSettings) BindFlags(fs *flag.FlagSet) {
	command.NewHelpFlag(fs, &set.HelpFlag)
	fs.StringVar(&set.serverMaddr, "maddr", daemon.ServiceMaddr, "listening socket maddr")
}

func Write() command.Command {
	const (
		name     = "write"
		synopsis = "Create or modify file data."
		usage    = "Placeholder text."
	)
	return command.MakeCommand(name, synopsis, usage, writeExecute)
}

func writeExecute(ctx context.Context, set *listSettings, args ...string) error {
	serverMaddr, err := multiaddr.NewMultiaddr(set.serverMaddr)
	if err != nil {
		return err
	}
	if len(args) != 2 {
		return fmt.Errorf("%w: missing filename and data", command.ErrUsage)
	}

	client, err := daemon.Connect(serverMaddr)
	if err != nil {
		log.Fatal(err)
	}

	var (
		filename = args[0]
		message  = args[1]
	)
	if err := motd.Write(client, filename, message); err != nil {
		return err
	}

	return ctx.Err()
}
