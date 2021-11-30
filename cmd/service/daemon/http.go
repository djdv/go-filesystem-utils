package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	cmds "github.com/ipfs/go-ipfs-cmds"
	cmdshttp "github.com/ipfs/go-ipfs-cmds/http"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

var ErrSubcommandsOnly = errors.New("only subcommands of this command are allowed")

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

func serveHTTP(ctx context.Context,
	listener manet.Listener, server *http.Server,
	shutdownTimeout time.Duration) <-chan error {
	errs := make(chan error)
	go func() {
		defer close(errs)
		serveErr := make(chan error)
		go func() {
			defer close(serveErr)
			err := server.Serve(manet.NetListener(listener))
			if err != nil &&
				!errors.Is(err, http.ErrServerClosed) {
				serveErr <- err
			}
		}()
		select {
		case err := <-serveErr:
			errs <- err
		case <-ctx.Done():
			sCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			if err := server.Shutdown(sCtx); err != nil {
				errs <- err
			}
			if err := <-serveErr; err != nil {
				errs <- err
			}
		}
	}()
	return errs
}

type serveResult struct {
	serverAddress multiaddr.Multiaddr
	serverErrs    <-chan error
	error
}

func listenAndServeCmdsHTTP(ctx context.Context, request *cmds.Request, runEnv *runEnv) (<-chan serveResult, error) {
	listenResults, err := generateListeners(ctx, request, runEnv.ServiceMaddrs...)
	if err != nil {
		return nil, err
	}

	serveResults := make(chan serveResult, cap(listenResults))
	go func() {
		defer close(serveResults)
		serverRoot := allowRemoteAccess(request.Root, request.Path)
		for result := range listenResults {
			if err := result.error; err != nil {
				serveResults <- serveResult{error: err}
				continue
			}
			const shutdownGrace = 30 * time.Second
			var (
				listener  = result.Listener
				server    = cmdsHTTPServer(serverRoot, runEnv.Environment)
				serveErrs = serveHTTP(ctx, listener, server, shutdownGrace)
			)
			serveResults <- serveResult{
				serverAddress: listener.Multiaddr(),
				serverErrs:    serveErrs,
			}
		}
	}()

	return serveResults, nil
}
