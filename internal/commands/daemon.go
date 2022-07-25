package commands

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/daemon"
	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type daemonSettings struct {
	serverMaddr string
	command.HelpFlag
}

func (set *daemonSettings) BindFlags(fs *flag.FlagSet) {
	command.NewHelpFlag(fs, &set.HelpFlag)
	fs.StringVar(&set.serverMaddr, "maddr", daemon.ServiceMaddr, "listening socket maddr")
}

func Daemon() command.Command {
	const (
		name     = "daemon"
		synopsis = "Hosts the service."
		usage    = "Placeholder text."
	)
	return command.MakeCommand(name, synopsis, usage, execute)
}

func execute(ctx context.Context, set *daemonSettings, _ ...string) error {
	serverMaddr, err := multiaddr.NewMultiaddr(set.serverMaddr)
	if err != nil {
		return err
	}
	socket, err := manet.Listen(serverMaddr)
	if err != nil {
		return err
	}

	const logProtocol = true // TODO: remove or add to CLI flags.
	var (
		serverOpts []p9.ServerOpt
		srvLog     = log.New(os.Stdout, "⬆️ server - ", log.Lshortfile)
		attacher   = daemon.NewRoot(socket)
	)
	if logProtocol {
		serverOpts = []p9.ServerOpt{p9.WithServerLogger(srvLog)}
	} else {
		srvLog.SetOutput(io.Discard)
	}

	// TODO: signalctx + shutdown on cancel

	var (
		goSocket = manet.NetListener(socket)
		server   = p9.NewServer(attacher, serverOpts...)
	)
	srvLog.Println("about to serve on:", socket.Addr().String())
	if err := server.Serve(goSocket); err != nil {
		if !attacher.Shutdown || !errors.Is(err, net.ErrClosed) {
			srvLog.Printf("serve err: %#v", err)
		}
	}
	return nil
}
