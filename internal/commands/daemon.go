package commands

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/daemon"
	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
)

type daemonSettings struct {
	serverMaddr multiaddr.Multiaddr
	commonSettings
	uid p9.UID
	gid p9.GID
}

func (set *daemonSettings) BindFlags(fs *flag.FlagSet) {
	set.commonSettings.BindFlags(fs)
	multiaddrVar(fs, &set.serverMaddr, "maddr",
		multiaddr.StringCast(daemon.ServiceMaddr), "Listening socket `maddr`.")
	// TODO: default should be current user ids on unix, NoUID on NT.
	uidVar(fs, &set.uid, "uid", p9.NoUID, "file owner's `uid`")
	gidVar(fs, &set.gid, "gid", p9.NoGID, "file owner's `gid`")
}

func Daemon() command.Command {
	const (
		name     = "daemon"
		synopsis = "Hosts the service."
		usage    = "Placeholder text."
	)
	return command.MakeCommand[*daemonSettings](name, synopsis, usage, daemonExecute)
}

func daemonExecute(ctx context.Context, set *daemonSettings) error {
	serverOpts := []daemon.ServerOption{
		daemon.WithUID(set.uid),
		daemon.WithGID(set.gid),
	}
	if set.verbose {
		// TODO: less fancy prefix and/or out+prefix from CLI flags
		serverLog := log.New(os.Stdout, "⬆️ server - ", log.Lshortfile)
		serverOpts = append(serverOpts, daemon.WithLogger[daemon.ServerOption](serverLog))
	} // TODO: else { log = null logger}

	// TODO: signalctx + shutdown on cancel

	server := daemon.NewServer(serverOpts...)
	if err := server.ListenAndServe(ctx, set.serverMaddr); err != nil {
		return err
	}

	// TODO: ferry server err on channel; default-select
	// if !err, printl "listening on..."; else fail

	return ctx.Err()
}
