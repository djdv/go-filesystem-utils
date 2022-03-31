package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/cmdslib/cmdsenv"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	cmds "github.com/ipfs/go-ipfs-cmds"
	cmdshttp "github.com/ipfs/go-ipfs-cmds/http"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

var ErrSubcommandsOnly = errors.New("only subcommands of this command are allowed")

type (
	serverListener struct {
		*http.Server
		manet.Listener
		// multiaddr.Multiaddr
	}
	serverInstance struct {
		//*http.Server
		serverListener
		errs errCh
	}
)

// TODO: English.
// allowRemoteAccess modifies leading commands in the component path,
// so that they may be called remotely.
// (Otherwise the cmds HTTP server will return 404 errors for subcommands we want to expose
// i.e. setting NoRemote on a parent command will 404 its subcommands inherently)
func allowRemoteAccess(root *cmds.Command, path []string) *cmds.Command {
	branch, err := root.Resolve(path)
	if err != nil {
		panic(err)
	}

	var newRoot, parent *cmds.Command
	for currentCommand, cmd := range branch {
		cmdCopy := *cmd
		cmd = &cmdCopy

		// Don't allow the original `Command.Subcommand` reference to be modified
		// make a copy.
		subcommands := make(map[string]*cmds.Command, len(cmd.Subcommands))
		for name, cmd := range cmd.Subcommands {
			subcommands[name] = cmd
		}
		cmd.Subcommands = subcommands

		if currentCommand == 0 {
			newRoot = cmd
			parent = newRoot
			continue
		}

		if cmd.NoRemote {
			cmd.PreRun = nil
			cmd.PostRun = nil
			cmd.Run = func(*cmds.Request, cmds.ResponseEmitter, cmds.Environment) error {
				return fmt.Errorf("%w via remote access", ErrSubcommandsOnly)
			}
			cmd.NoRemote = false

			// Replace the reference in our parent
			// so that it points to this modified child copy.
			childName := path[currentCommand-1]
			parent.Subcommands[childName] = cmd
		}

		parent = cmd
	}

	return newRoot
}

func cmdsHTTPServer(serverRoot *cmds.Command, serverEnv cmds.Environment) *http.Server {
	return &http.Server{
		Handler: cmdshttp.NewHandler(
			serverEnv, serverRoot,
			cmdshttp.NewServerConfig()),
	}
}

func generateServers(ctx context.Context, request *cmds.Request,
	serviceEnv cmdsenv.Environment, listeners listeners,
) (<-chan serverListener, errCh) {
	var (
		serverInstances = make(chan serverListener, cap(listeners))
		errs            = make(chan error)
	)
	go func() {
		defer close(serverInstances)
		defer close(errs)
		var (
			serverRoot = allowRemoteAccess(request.Root, request.Path)
			serve      = func(listener manet.Listener) (serverListener, error) {
				instance := serverListener{
					Server:   cmdsHTTPServer(serverRoot, serviceEnv),
					Listener: listener,
				}
				return instance, nil
			}
		)
		ProcessResults(ctx, listeners, serverInstances, errs, serve)
	}()
	return serverInstances, errs
}

func startServers(ctx context.Context,
	listeners <-chan serverListener,
) <-chan serverInstance {
	servers := make(chan serverInstance, cap(listeners))
	go func() {
		defer close(servers)
		serve := func(server serverListener) (serverInstance, error) {
			instance := serverInstance{
				serverListener: server,
				errs:           serveHTTP(ctx, server),
			}
			return instance, nil
		}
		ProcessResults(ctx, listeners, servers, nil, serve)
	}()
	return servers
}

// TODO needs emitter? "shutting down: $maddr"
// ^ caller should do it, emit it when they receive the result
func shutdownServers(ctx context.Context, timeout time.Duration,
	servers <-chan serverInstance,
) (maddrs, errCh) {
	// combine Server and Shutdown error
	// filter out expected http.Error (shutting down or whatever)
	// return either the maddr or error on the channel

	// Caller must not cancel us, so all shutdowns complete
	var (
		maddrs = make(chan multiaddr.Multiaddr, cap(servers))
		errs   = make(chan error)
	)
	go func() {
		defer close(maddrs)
		defer close(errs)
		shutdown := func(instance serverInstance) (multiaddr.Multiaddr, error) {
			var (
				maddr           = instance.Multiaddr()
				serveErrs       = instance.errs
				timeout, cancel = context.WithTimeout(ctx, timeout)
			)
			defer cancel()
			err := instance.Shutdown(timeout)
			for serveErr := range serveErrs {
				err = maybeWrapErr(err, serveErr)
			}
			return maddr, err
		}
		ProcessResults(ctx, servers, maddrs, errs, shutdown)
	}()

	return maddrs, errs
}

func serveHTTP(ctx context.Context,
	serverListener serverListener,
) <-chan error {
	var (
		listener = serverListener.Listener
		server   = serverListener.Server
		serveErr = make(chan error)
	)
	go func() {
		defer close(serveErr)
		var (
			stdListener = manet.NetListener(listener)
			err         = server.Serve(stdListener)
		)
		if err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			select {
			case serveErr <- err:
			case <-ctx.Done():
			}
		}
	}()
	return serveErr
}
