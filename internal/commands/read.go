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
type readSettings struct {
	serverMaddr string
	command.HelpFlag
}

func (set *readSettings) BindFlags(fs *flag.FlagSet) {
	command.NewHelpFlag(fs, &set.HelpFlag)
	fs.StringVar(&set.serverMaddr, "maddr", daemon.ServiceMaddr, "listening socket maddr")
}

func Read() command.Command {
	const (
		name     = "read"
		synopsis = "Read existing files."
		usage    = "Placeholder text."
	)
	return command.MakeCommand(name, synopsis, usage, readExecute)
}

func readExecute(ctx context.Context, set *listSettings, args ...string) error {
	serverMaddr, err := multiaddr.NewMultiaddr(set.serverMaddr)
	if err != nil {
		return err
	}
	if len(args) != 1 {
		return fmt.Errorf("%w: missing filename", command.ErrUsage)
	}

	client, err := daemon.Connect(serverMaddr)
	if err != nil {
		log.Fatal(err)
	}

	filename := args[0]

	message, err := motd.Read(client, filename)
	if err != nil {
		return err
	}
	fmt.Printf("%s: %s\n", filename, message)

	return ctx.Err()
}
