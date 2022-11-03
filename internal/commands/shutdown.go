package commands

import (
	"context"
	"log"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/daemon"
	"github.com/multiformats/go-multiaddr"
)

type shutdownSettings struct{ clientSettings }

func Shutdown() command.Command {
	const (
		name     = "shutdown"
		synopsis = "Stop the service."
		usage    = "Placeholder text."
	)
	return command.MakeCommand[*shutdownSettings](name, synopsis, usage, shutdownExecute)
}

func shutdownExecute(ctx context.Context, set *shutdownSettings, _ ...string) error {
	var clientOpts []daemon.ClientOption
	if set.verbose {
		// TODO: less fancy prefix and/or out+prefix from CLI flags
		clientLog := log.New(os.Stdout, "⬇️ client - ", log.Lshortfile)
		clientOpts = append(clientOpts, daemon.WithLogger(clientLog))
	}

	// TODO: signalctx + shutdown on cancel

	serviceMaddr := set.serviceMaddr
	if lazy, ok := serviceMaddr.(lazyFlag[multiaddr.Multiaddr]); ok {
		var err error
		if serviceMaddr, err = lazy.get(); err != nil {
			return err
		}
	}

	client, err := daemon.Connect(serviceMaddr, clientOpts...)
	if err != nil {
		return err
	}
	if err := client.Shutdown(serviceMaddr); err != nil {
		return err
	}

	return ctx.Err()
}