package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	cmds "github.com/ipfs/go-ipfs-cmds"
	cmdshttp "github.com/ipfs/go-ipfs-cmds/http"
	manet "github.com/multiformats/go-multiaddr/net"
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
				return errors.New("only subcommands of this command are allowed via remote access")
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

func httpServerFromCmds(serverRoot *cmds.Command, serverEnv cmds.Environment) *http.Server {
	return &http.Server{
		Handler: cmdshttp.NewHandler(serverEnv,
			serverRoot, cmdshttp.NewServerConfig()),
	}
}

// TODO: review
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
				fmt.Println("DBG: srv err:", err)
				serveErr <- err
			}
		}()
		select {
		case err := <-serveErr:
			errs <- err
		case <-ctx.Done():
			err := shutdownServer(server, shutdownTimeout)
			if err != nil {
				errs <- err
			}
		}
	}()
	return errs
}

func shutdownServer(server *http.Server, timeout time.Duration) error {
	timerCtx, timerCancel := context.WithTimeout(context.Background(), timeout)
	defer timerCancel()
	err := server.Shutdown(timerCtx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("could not shutdown server before timeout (%s): %w",
				timeout, err,
			)
		}
		return err
	}

	return nil
}

/*
func serveCmdsHTTP(ctx context.Context,
	listener manet.Listener, clientRoot *cmds.Command,
	env cmds.Environment) <-chan error {
	var (
		httpServer = &http.Server{
			Handler: cmdshttp.NewHandler(env,
				clientRoot, cmdshttp.NewServerConfig()),
		}
		errs = make(chan error)
	)
	go func() {
		const stopGrace = 30 * time.Second
		defer close(errs)

		// The actual listen and serve / accept loop.
		serveErr := make(chan error, 1)
		go func() {
			defer close(serveErr)
			serveErr <- httpServer.Serve(manet.NetListener(listener))
		}()

		// Context handling to cancel the server mid `Serve`,
		// and relay errors.
		select {
		case err := <-serveErr:
			errs <- err
		case <-ctx.Done():
			// TODO: shutdown grace should come from args
			timeout, timeoutCancel := context.WithTimeout(context.Background(),
				stopGrace/2)
			defer timeoutCancel()
			if err := httpServer.Shutdown(timeout); err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					err = fmt.Errorf("could not shutdown server before timeout (%s): %w",
						timeout, err,
					)
				}
				errs <- err
			}

			// Serve routine must return now.
			if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
				errs <- err
			}
		}
	}()

	return errs
}
*/
