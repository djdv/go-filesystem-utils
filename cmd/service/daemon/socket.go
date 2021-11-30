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

type closer func() error

func (fn closer) Close() error { return fn() }

type listenResult struct {
	manet.Listener
	error
}

func listenersFromCmds(ctx context.Context, request *cmds.Request,
	maddrs ...multiaddr.Multiaddr) (<-chan listenResult, error) {
	results, err := listenersFromCmdsExtra(ctx, request.Command.Extra)
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

// TODO: We need a way to pass listeners from `service` to `daemon`.
// Easiest way would be to add a parameter for it of type `[]net.Listener`.
// This would need to be of typed as `[]string` in the cmds.Option,
// which could be in a specific format.
// Anything usable with `os.NewFile`->`net.FileListener`.
// E.g.
// CSV of `file-descriptor $groupseperator  file-name`
// As `fs.exe service daemon --existing-sockets="3:systemdSock1,4:systemdSock2"`
//
// For now, we can sidestep this at the API level and just use command.Extra
// (from within `service`, after copying `daemon` add the listeners, then call execute on that)
// But this is not a proper solution, it's only temporary to not break the existing feature
// while separating the commands implementations.
func listenersFromCmdsExtra(ctx context.Context,
	cmdsExtra *cmds.Extra) (<-chan listenResult, error) {
	cmdsListeners, provided := cmdsExtra.GetValue("magic")
	if !provided {
		return nil, nil
	}
	listeners, ok := cmdsListeners.([]manet.Listener)
	if !ok {
		return nil, fmt.Errorf(
			"Command.Extra value has wrong type"+
				"\n\texpected %T"+
				"\n\tgot: %T",
			listeners,
			cmdsListeners,
		)
	}

	// TODO: replace this direct copy to something like (fd=>listener)
	// caller passes as --someArg=fd1:unix,fd2:tcp,...
	// maybe --unix-listeners=fd1,--tcp-listeners=fd2,...
	// the caller would parse this and pass it to us.
	// Like (unix,tcp,udp []fdint) or something.
	// NOTE: Currently, no error is possible
	// since listeners are passed and asserted directly.
	// This will likely change when going from
	// fd (int) -> go interface assertions, done for each fd.
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
