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

// FIXME [deadlock]: this returns a nil errchan when listeners is canceled early / empty.
func setupCmdsHTTP(ctx context.Context, root *cmds.Command,
	env cmds.Environment, pairs <-chan listenerPair) <-chan error {
	var errs <-chan error
	for pair := range pairs {
		httpServerErrs := serveHTTPThenCleanup(ctx, pair, root, env)
		if httpServerErrs == nil {
			fmt.Println("⚠️  httpServerErrs 1 was nil")
		}
		if errs == nil {
			errs = httpServerErrs
		} else {
			errs = mergeErrs(errs, httpServerErrs)
		}
	}
	// HACK: Rework the function instead of doing this.
	if errs == nil {
		// fmt.Println("⚠️  httpServerErrs 2[h] was nil")
		empty := make(chan error)
		close(empty)
		errs = empty
	}
	return errs
}

func serveHTTPThenCleanup(ctx context.Context, pair listenerPair,
	root *cmds.Command, env cmds.Environment) <-chan error {
	var (
		errs       = make(chan error, 2)
		serverErrs = acceptCmdsHTTP(ctx, pair.Listener, root, env)
		cleanup    = pair.cleanupFunc
	)
	go func() {
		defer close(errs)
		var err error
		for serverErr := range serverErrs {
			if err == nil {
				err = fmt.Errorf("HTTP server error: %w", serverErr)
			} else {
				err = fmt.Errorf("%w - %s", err, serverErr)
			}
		}
		if err != nil {
			errs <- err
		}

		// When done (after Serve() returns; closing the listener)
		// call this listeners cleanup (if it has one)
		if cleanup != nil {
			if err := cleanup(); err != nil {
				err = fmt.Errorf("listener cleanup: %w", err)
				errs <- err
			}
		}
	}()

	return errs
}

func emitAndRelayListeners(emitter cmds.ResponseEmitter,
	listeners <-chan listenerPair) (<-chan listenerPair, <-chan error) {
	var (
		relay = make(chan listenerPair, cap(listeners)+1)
		errs  = make(chan error, 1)
	)
	go func() {
		defer close(relay)
		defer close(errs)
		for listener := range listeners {
			err := emitMaddrListener(emitter, listener.Multiaddr())
			if err != nil {
				if cErr := listener.Close(); cErr != nil {
					err = fmt.Errorf(
						"%w - failed to close listener: %s",
						err, cErr,
					)
				}
				errs <- err
				return
			}
			relay <- listener
		}
	}()
	return relay, errs
}

func acceptCmdsHTTP(ctx context.Context,
	listener manet.Listener, clientRoot *cmds.Command,
	env cmds.Environment) (serverErrs <-chan error) {
	var (
		httpServer = &http.Server{
			Handler: cmdshttp.NewHandler(env,
				clientRoot, cmdshttp.NewServerConfig()),
		}
		httpServerErrs = make(chan error)
	)
	go func() {
		const stopGrace = 30 * time.Second
		defer close(httpServerErrs)

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
			httpServerErrs <- err
		case <-ctx.Done():
			timeout, timeoutCancel := context.WithTimeout(context.Background(),
				stopGrace/2)
			defer timeoutCancel()
			if err := httpServer.Shutdown(timeout); err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					err = fmt.Errorf("could not shutdown server before timeout (%s): %w",
						timeout, err,
					)
				}
				httpServerErrs <- err
			}

			// Serve routine must return now.
			if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
				httpServerErrs <- err
			}
		}
	}()

	return httpServerErrs
}
