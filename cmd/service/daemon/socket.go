package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

func listenersFromMaddrs(ctx context.Context,
	maddrs ...multiaddr.Multiaddr) (<-chan manet.Listener, <-chan error) {
	var (
		listeners = make(chan manet.Listener, len(maddrs))
		errs      = make(chan error, 1)
	)
	go func() {
		defer close(listeners)
		defer close(errs)
		for _, maddr := range maddrs {
			listener, err := manet.Listen(maddr)
			if err != nil {
				select {
				case errs <- err:
					continue
				case <-ctx.Done():
					return
				}
			}
			select {
			case listeners <- listener:
			case <-ctx.Done():
				if cErr := listener.Close(); cErr != nil {
					errs <- fmt.Errorf("failed to close listener: %w", cErr)
				}
				return
			}
		}
	}()
	return listeners, errs
}

func listenersFromRequest(request *cmds.Request) (<-chan manet.Listener, error) {
	return listenersFromCmdsExtra(request.Command.Extra)
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
func listenersFromCmdsExtra(cmdsExtra *cmds.Extra) (<-chan manet.Listener, error) {
	cmdsListeners, listenersProvided := cmdsExtra.GetValue("magic")
	if !listenersProvided {
		return nil, nil
	}
	manetListeners, ok := cmdsListeners.([]manet.Listener)
	if !ok {
		err := fmt.Errorf("Command.Extra value has wrong type"+
			"expected %T"+
			"got: %T",
			manetListeners,
			cmdsListeners,
		)
		return nil, err
	}

	// TODO: replace this direct copy to something like (fd=>listener)
	// caller passes as --someArg=fd1:unix,fd2:tcp,...
	// maybe --unix-listeners=fd1,--tcp-listeners=fd2,...
	// we'd then parse that request here.
	// ^ not here; do it in the caller, pass it to us. (unix,tcp,udp []fdint)
	listeners := make(chan manet.Listener, len(manetListeners))
	go func() {
		defer close(listeners)
		for _, listener := range manetListeners {
			listeners <- listener
		}
	}()

	return listeners, nil
}

type listenerPair struct {
	manet.Listener
	cleanupFunc
}

// wraps listener with nil cleanupFunc
func listenersToPairs(input <-chan manet.Listener) <-chan listenerPair {
	relay := make(chan listenerPair, cap(input))
	go func() {
		for value := range input {
			relay <- listenerPair{Listener: value}
		}
	}()
	return relay
}

func getListeners(ctx context.Context, request *cmds.Request,
	maddrs ...multiaddr.Multiaddr) (<-chan listenerPair, <-chan error, error) {
	var (
		listeners      <-chan manet.Listener
		errs           <-chan error
		appendListener = func(head <-chan manet.Listener) {
			if listeners == nil {
				listeners = head
			} else {
				listeners = mergeListeners(listeners, head)
			}
		}
	)

	cmdsListeners, err := listenersFromRequest(request)
	if err != nil {
		return nil, nil, err
	}
	if cmdsListeners != nil {
		appendListener(cmdsListeners)
	}

	if len(maddrs) > 0 {
		argListeners, argErrs := listenersFromMaddrs(ctx, maddrs...)
		appendListener(argListeners)
		errs = argErrs
	}

	if listeners == nil {
		return defaultListeners(ctx)
	}

	pairs := listenersToPairs(listeners)
	if errs == nil {
		// HACK: errs == nil handling should be refactored
		// (reworked or obviated)
		empty := make(chan error)
		close(empty)
		errs = empty
	}
	return pairs, errs, nil
}

func mergeListeners(sources ...<-chan manet.Listener) <-chan manet.Listener {
	if len(sources) == 1 {
		return sources[0]
	}

	type (
		source   = <-chan manet.Listener
		sourceRw = chan manet.Listener
	)
	var (
		mergedWg  sync.WaitGroup
		mergedCh  = make(sourceRw)
		mergeFrom = func(ch source) {
			for value := range ch {
				mergedCh <- value
			}
			mergedWg.Done()
		}
	)

	mergedWg.Add(len(sources))
	go func() { mergedWg.Wait(); close(mergedCh) }()

	for _, source := range sources {
		go mergeFrom(source)
	}

	return mergedCh
}

func defaultListeners(ctx context.Context) (<-chan listenerPair, <-chan error, error) {
	maddrs, err := UserServiceMaddrs()
	if err != nil {
		return nil, nil, err
	}
	var (
		pairs = make(chan listenerPair, len(maddrs))
		errs  = make(chan error, 1)
	)
	go func() {
		defer close(pairs)
		defer close(errs)
		for _, maddr := range maddrs {
			pair, err := listenerPairFromMaddr(maddr)
			if err != nil {
				errs <- err
				continue
			}
			select {
			case pairs <- *pair:
			case <-ctx.Done():
				err := returnErrWithCleanup(
					pair.Listener.Close(),
					pair.cleanupFunc)
				if err != nil {
					errs <- err
				}
			}
			// NOTE: While the API allows for multiple listeners -
			// we don't have a reason to use more than 1 in the default case.
			return
		}
	}()

	return pairs, errs, nil
}

func listenerPairFromMaddr(maddr multiaddr.Multiaddr) (*listenerPair, error) {
	cleanup, err := prepareHostForSocket(maddr)
	if err != nil {
		return nil, err
	}

	listener, err := manet.Listen(maddr)
	if err != nil {
		return nil, returnErrWithCleanup(err, cleanup)
	}
	return &listenerPair{
		Listener:    listener,
		cleanupFunc: cleanup,
	}, nil
}

func returnErrWithCleanup(err error, cleanup cleanupFunc) error {
	if cleanup == nil {
		return err
	}
	if cErr := cleanup(); cErr != nil {
		cErr = fmt.Errorf("couldn't cleanup: %w", cErr)
		if err == nil {
			return cErr
		}
		return fmt.Errorf("%w - %s", err, cErr)
	}
	return err
}

// maybeGetUnixSocketPath returns the path
// of the first Unix domain socket within the multiaddr (if any)
func maybeGetUnixSocketPath(ma multiaddr.Multiaddr) (target string, hadUnixComponent bool) {
	multiaddr.ForEach(ma, func(comp multiaddr.Component) bool {
		if hadUnixComponent = comp.Protocol().Code == multiaddr.P_UNIX; hadUnixComponent {
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

func prepareHostForSocket(maddr multiaddr.Multiaddr) (cleanupFunc, error) {
	unixSocketPath, hadUnixSocket := maybeGetUnixSocketPath(maddr)
	if !hadUnixSocket {
		return nil, nil
	}

	// TODO: switch this back to regular Stat when this is merged
	// https://go-review.googlesource.com/c/go/+/338069/
	if _, err := os.Lstat(unixSocketPath); err == nil {
		return nil, fmt.Errorf(
			"socket file already exists: \"%s\"",
			unixSocketPath)
	}

	parent := filepath.Dir(unixSocketPath)
	if err := os.MkdirAll(parent, 0o775); err != nil {
		return nil, fmt.Errorf(
			"can't create socket's parent directory"+
				"\n\t(\"%s\")"+
				"\n\t%w",
			unixSocketPath, err)
	}
	cleanup := func() error { return os.Remove(parent) }

	return cleanup, nil
}
