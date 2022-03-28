package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	. "github.com/djdv/go-filesystem-utils/internal/generic"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

const (
	// TODO: better value for this?
	asciiENQ           = 0x5
	cmdsSocketKey byte = asciiENQ
)

type (
	maddrs    <-chan multiaddr.Multiaddr
	listeners = <-chan manet.Listener

	listenResult struct {
		manet.Listener
		error
	}

	closer func() error
)

func (fn closer) Close() error { return fn() }

func generate[in any](ctx context.Context, inputs ...in) <-chan in {
	out := make(chan in, len(inputs))
	go func() {
		defer close(out)
		for _, element := range inputs {
			select {
			case out <- element:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out

}

func listenersFromMaddrs(ctx context.Context, maddrs maddrs) (listeners, errCh) {
	var (
		listeners = make(chan manet.Listener, cap(maddrs))
		errs      = make(chan error)
	)
	go func() {
		defer close(listeners)
		defer close(errs)
		for maddr := range maddrs {
			listener, err := manet.Listen(maddr)
			if err != nil {
				select {
				case errs <- err:
				case <-ctx.Done():
				}
				return
			}
			select {
			case listeners <- listener:
			case <-ctx.Done():
				return
			}
		}
	}()
	return listeners, errs
}

func initializeAndListen(ctx context.Context, maddrs maddrs) (listeners, errCh) {
	//subCtx, cancel := context.WithCancel(ctx)
	var (
		netMaddrs           = make(chan multiaddr.Multiaddr)
		netSockets, netErrs = listenersFromMaddrs(ctx, netMaddrs)

		udsMaddrs           = make(chan multiaddr.Multiaddr)
		udsSockets, udsErrs = initalizeAndListenUnixMaddrs(ctx, udsMaddrs)

		errChans = []errCh{netErrs, udsErrs}
	)
	go func() {
		defer close(netMaddrs)
		defer close(udsMaddrs)
		//defer cancel()
		route := func(maddr multiaddr.Multiaddr) error {
			relay := netMaddrs
			for _, protocol := range maddr.Protocols() {
				if protocol.Code == multiaddr.P_UNIX {
					relay = udsMaddrs
				}
			}
			select {
			case relay <- maddr:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		ForEachOrError(ctx, maddrs, nil, route)
	}()
	return CtxMerge(ctx, udsSockets, netSockets), CtxMerge(ctx, errChans...)
}

func initalizeAndListenUnixMaddrs(ctx context.Context, maddrs maddrs) (listeners, errCh) {
	// TODO: filter up front
	// If maddr paths do not exist, or we can't makedir (not mkdirall)
	// don't use it. Return all errs, when all non-usable.
	// otherwise, return first valid path.
	var (
		directories, directoryErrs = serviceDirs(ctx, maddrs)
		listeners, listenerErrs    = listenersFromCloserMaddrs(ctx, directories)
	)
	return listeners, CtxMerge(ctx, directoryErrs, listenerErrs)
}

func serviceDirs(ctx context.Context, maddrs maddrs) (<-chan maddrWithCloser, errCh) {
	var (
		directories = make(chan maddrWithCloser, cap(maddrs))
		errs        = make(chan error)
	)
	go func() {
		defer close(directories)
		defer close(errs)
		const permissions = 0o775
		mkDir := func(maddr multiaddr.Multiaddr) (maddrWithCloser, error) {
			var (
				unixSocketPath        = getFirstUnixSocketPath(maddr)
				socketDir, socketName = filepath.Split(unixSocketPath)
			)
			if err := os.Mkdir(socketDir, permissions); err != nil {
				var (
					shortDir  = filepath.Base(socketDir)
					shortName = filepath.Join(shortDir, socketName)
				)
				err = fmt.Errorf("tried to make socket parent for `%s`: %w", shortName, err)
				return nil, err
			}
			var (
				rmDir     closer          = func() error { return os.Remove(socketDir) }
				directory maddrWithCloser = struct {
					multiaddr.Multiaddr
					io.Closer
				}{
					Multiaddr: maddr,
					Closer:    rmDir,
				}
			)
			return directory, nil
		}
		ProcessResults(ctx, maddrs, directories, errs, mkDir)
	}()
	return directories, errs
}

type maddrWithCloser interface {
	multiaddr.Multiaddr
	io.Closer
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

func listenersFromCloserMaddrs(ctx context.Context,
	closers <-chan maddrWithCloser) (listeners, errCh) {
	var (
		listeners = make(chan manet.Listener, cap(closers))
		errs      = make(chan error)
	)
	go func() {
		defer close(listeners)
		defer close(errs)
		listenOrClose := func(maddrCloser maddrWithCloser) (manet.Listener, error) {
			var (
				cleanup       = maddrCloser.Close
				listener, err = manet.Listen(maddrCloser)
			)
			if err != nil {
				if cErr := cleanup(); cErr != nil {
					err = fmt.Errorf("%w - could not cleanup for socket: %s",
						err, cErr)
				}
				return nil, err
			}
			listenerCloser := listenerWithCleanup{
				Listener: listener,
				closer:   closer(cleanup),
			}
			return listenerCloser, nil
		}
		ProcessResults(ctx, closers, listeners, errs, listenOrClose)
	}()
	return listeners, errs
}

func UseHostListeners(request *cmds.Request, listeners []manet.Listener) error {
	request.SetOption(string(cmdsSocketKey), listeners)
	return nil
}

func hostListenersFromRequest(request *cmds.Request) ([]manet.Listener, error) {
	cmdsListeners, provided := request.Options[string(cmdsSocketKey)]
	if !provided {
		return nil, nil
	}
	listeners, ok := cmdsListeners.([]manet.Listener)
	if ok {
		return listeners, nil
	}
	err := fmt.Errorf(
		"request value has wrong type"+
			"\n\tgot: %T"+
			"\n\twant: %T",
		cmdsListeners,
		listeners,
	)
	return nil, err
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
