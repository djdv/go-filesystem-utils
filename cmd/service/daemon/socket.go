package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

const (
	asciiENQ           = 0x5
	cmdsSocketKey byte = asciiENQ
)

type closer func() error

func (fn closer) Close() error { return fn() }

type listenResult struct {
	manet.Listener
	error
}

func listenersFromCmds(ctx context.Context, request *cmds.Request,
	maddrs ...multiaddr.Multiaddr) (<-chan listenResult, error) {
	results, err := hostListenersFromRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	if len(maddrs) != 0 {
		argListeners := listenersFromMaddrs(generateMaddrs(ctx, maddrs...))
		if results == nil {
			results = argListeners
		} else {
			results = mergeListenerResults(results, argListeners)
		}
	}
	return results, nil
}

func generateMaddrs(ctx context.Context,
	maddrs ...multiaddr.Multiaddr) <-chan multiaddr.Multiaddr {
	maddrChan := make(chan multiaddr.Multiaddr, len(maddrs))
	go func() {
		defer close(maddrChan)
		for _, maddr := range maddrs {
			select {
			case maddrChan <- maddr:
			case <-ctx.Done():
				return
			}
		}
	}()
	return maddrChan
}

func listenersFromMaddrs(maddrs <-chan multiaddr.Multiaddr) <-chan listenResult {
	results := make(chan listenResult, cap(maddrs))
	go func() {
		defer close(results)
		for maddr := range maddrs {
			listener, err := manet.Listen(maddr)
			results <- listenResult{Listener: listener, error: err}
		}
	}()
	return results
}

func initializeAndListen(maddrs <-chan multiaddr.Multiaddr) <-chan listenResult {
	var (
		netMaddrs  = make(chan multiaddr.Multiaddr)
		netSockets = listenersFromMaddrs(netMaddrs)

		udsMaddrs  = make(chan multiaddr.Multiaddr)
		udsSockets = initalizeAndListenUnixMaddrs(udsMaddrs)
	)
	go func() {
		defer close(netMaddrs)
		defer close(udsMaddrs)
		for maddr := range maddrs {
			relay := netMaddrs
			for _, protocol := range maddr.Protocols() {
				if protocol.Code == multiaddr.P_UNIX {
					relay = udsMaddrs
				}
			}
			relay <- maddr
		}
	}()
	return mergeListenerResults(udsSockets, netSockets)
}

func initalizeAndListenUnixMaddrs(maddrs <-chan multiaddr.Multiaddr) <-chan listenResult {
	return listenersFromCloserMaddrs(directoryClosersFromMaddrs(maddrs))
}

type listenerWithCleanup struct {
	manet.Listener
	closer io.Closer
}

func (listener listenerWithCleanup) Close() error {
	err := listener.Listener.Close()
	if err != nil {
		err = fmt.Errorf("could not close listener: %w", err)
	}
	if cErr := listener.closer.Close(); cErr != nil {
		cErr = fmt.Errorf("could not cleanup after listener: %w", err)
		if err == nil {
			return cErr
		}
		return fmt.Errorf("%w\n\t%s", err, cErr)
	}
	return err
}

func listenersFromCloserMaddrs(closerResults <-chan maddrWithCloser) <-chan listenResult {
	results := make(chan listenResult, cap(closerResults))
	go func() {
		defer close(results)
		for result := range closerResults {
			if err := result.error; err != nil {
				results <- listenResult{error: err}
				continue
			}
			var (
				closer  = result.Closer
				cleanup = closer.Close
				maddr   = result.Multiaddr
			)
			listener, err := manet.Listen(maddr)
			if err != nil {
				if cErr := cleanup(); cErr != nil {
					err = fmt.Errorf("%w - could not cleanup for socket: %s",
						err, cErr)
				}
			}
			results <- listenResult{
				Listener: listenerWithCleanup{
					Listener: listener,
					closer:   closer,
				},
				error: err,
			}
		}
	}()
	return results
}

type maddrWithCloser struct {
	multiaddr.Multiaddr
	io.Closer
	error
}

func directoryClosersFromMaddrs(maddrs <-chan multiaddr.Multiaddr) <-chan maddrWithCloser {
	results := make(chan maddrWithCloser, cap(maddrs))
	go func() {
		defer close(results)
		for maddr := range maddrs {
			const permissions = 0o775
			var (
				unixSocketPath = getFirstUnixSocketPath(maddr)
				socketDir      = filepath.Dir(unixSocketPath)
			)
			results <- maddrWithCloser{
				Multiaddr: maddr,
				error:     os.Mkdir(socketDir, permissions),
				Closer:    closer(func() error { return os.Remove(socketDir) }),
			}
		}
	}()
	return results
}

func mergeListenerResults(sources ...<-chan listenResult) <-chan listenResult {
	type (
		source   = <-chan listenResult
		sourceRw = chan listenResult
	)
	var (
		mergedWg  sync.WaitGroup
		mergedCh  = make(sourceRw)
		mergeFrom = func(ch source) {
			defer mergedWg.Done()
			for value := range ch {
				mergedCh <- value
			}
		}
	)
	mergedWg.Add(len(sources))
	for _, source := range sources {
		go mergeFrom(source)
	}
	go func() { mergedWg.Wait(); close(mergedCh) }()

	return mergedCh
}

func generateListeners(ctx context.Context, request *cmds.Request,
	maddrs ...multiaddr.Multiaddr) (<-chan listenResult, error) {
	listeners, err := listenersFromCmds(ctx, request, maddrs...)
	if err != nil {
		return nil, err
	}
	if listeners != nil {
		return listeners, nil
	}
	return listenersFromDefaults(ctx)
}

func UseHostListeners(request *cmds.Request, listeners []manet.Listener) error {
	request.Options[string(cmdsSocketKey)] = listeners
	return nil
}

func hostListenersFromRequest(ctx context.Context,
	request *cmds.Request) (<-chan listenResult, error) {
	cmdsListeners, provided := request.Options[string(cmdsSocketKey)]
	if !provided {
		return nil, nil
	}
	listeners, ok := cmdsListeners.([]manet.Listener)
	if !ok {
		return nil, fmt.Errorf(
			"request value has wrong type"+
				"\n\tgot: %T"+
				"\n\twant: %T",
			cmdsListeners,
			listeners,
		)
	}
	if len(listeners) == 0 {
		return nil, nil
	}

	results := make(chan listenResult, len(listeners))
	go func() {
		defer close(results)
		for _, listener := range listeners {
			select {
			case results <- listenResult{Listener: listener}:
			case <-ctx.Done():
				return
			}
		}
	}()

	return results, nil
}

func listenersFromDefaults(ctx context.Context) (<-chan listenResult, error) {
	maddrs, err := UserServiceMaddrs()
	if err != nil {
		return nil, err
	}
	return initializeAndListen(generateMaddrs(ctx, maddrs...)), nil
}

// getFirstUnixSocketPath returns the path
// of the first Unix domain socket within the multiaddr (if any)
func getFirstUnixSocketPath(ma multiaddr.Multiaddr) (target string) {
	multiaddr.ForEach(ma, func(comp multiaddr.Component) bool {
		isUnixComponent := comp.Protocol().Code == multiaddr.P_UNIX
		if isUnixComponent {
			target = comp.Value()
			if runtime.GOOS == "windows" { // `/C:\path` -> `C:\path`
				target = strings.TrimPrefix(target, `/`)
			}
			return true
		}
		return false
	})
	return
}
