package commands

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/daemon"
	"github.com/multiformats/go-multiaddr"
)

type (
	unmountSettings struct {
		clientSettings
		all bool
	}
)

func (set *unmountSettings) BindFlags(flagSet *flag.FlagSet) {
	set.clientSettings.BindFlags(flagSet)
	flagSet.BoolVar(&set.all, "a", false, "placeholder text")
}

func Unmount() command.Command {
	const (
		name     = "unmount"
		synopsis = "Unmount file systems."
		usage    = "Placeholder text."
	)
	return command.MakeCommand[*unmountSettings](name, synopsis, usage, unmountExecute)
}

func unmountExecute(ctx context.Context, set *unmountSettings) error {
	var (
		err          error
		serviceMaddr = set.serviceMaddr

		client     *daemon.Client
		clientOpts []daemon.ClientOption
	)
	if lazy, ok := serviceMaddr.(lazyFlag[multiaddr.Multiaddr]); ok {
		if serviceMaddr, err = lazy.get(); err != nil {
			return err
		}
	}
	if set.verbose {
		// TODO: less fancy prefix and/or out+prefix from CLI flags
		clientLog := log.New(os.Stdout, "⬇️ client - ", log.Lshortfile)
		clientOpts = append(clientOpts, daemon.WithLogger(clientLog))
	}

	// TODO: don't launch if we can't connect.
	if serviceMaddr != nil {
		client, err = daemon.Connect(serviceMaddr, clientOpts...)
	} else {
		client, err = daemon.ConnectOrLaunchLocal(clientOpts...)
	}
	if err != nil {
		return err
	}
	all := set.all
	if !all {
		return errors.New("single targets not implemented yet, use `-a`")
	}
	unmountOpts := []daemon.UnmountOption{
		daemon.UnmountAll(all),
	}
	if err := client.Unmount(ctx, unmountOpts...); err != nil {
		return err
	}
	if err := client.Close(); err != nil {
		return err
	}
	return ctx.Err()
}
