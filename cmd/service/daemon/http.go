package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	cmds "github.com/ipfs/go-ipfs-cmds"
	cmdshttp "github.com/ipfs/go-ipfs-cmds/http"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

func serveCmdsHTTP(ctx context.Context,
	cmdsRoot *cmds.Command, cmdsEnv cmds.Environment,
	listeners ...manet.Listener) (<-chan multiaddr.Multiaddr, <-chan error) {
	var (
		listenersLen = len(listeners)
		maddrs       = make(chan multiaddr.Multiaddr, listenersLen)
	)
	defer close(maddrs)
	if listenersLen == 0 {
		return maddrs, nil
	}

	var (
		serveWg   sync.WaitGroup
		serveErrs = make(chan error)
	)
	for _, listener := range listeners {
		serverErrs := acceptCmdsHTTP(ctx, listener, cmdsRoot, cmdsEnv)
		// Aggregate server-errors into serve-errors.
		serveWg.Add(1)
		go func() {
			defer serveWg.Done()
			for err := range serverErrs {
				err = fmt.Errorf("HTTP server error: %w", err)
				serveErrs <- err
			}
		}()

		maddrs <- listener.Multiaddr() // Tell the caller this server is ready.
	}

	// Close serveErrs after all aggregate servers close.
	go func() { defer close(serveErrs); serveWg.Wait() }()

	return maddrs, serveErrs
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
		serveErr := make(chan error)

		// The actual listen and serve / accept loop.
		go func() { serveErr <- httpServer.Serve(manet.NetListener(listener)) }()

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
