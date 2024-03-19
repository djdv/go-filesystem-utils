package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/adrg/xdg"
	p9net "github.com/djdv/go-filesystem-utils/internal/net/9p"
	"github.com/djdv/p9/p9"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/u-root/uio/ulog"
)

type (
	serveFunc = func(manet.Listener) error
)

func makeServer(fsys p9.Attacher, log ulog.Logger) *p9net.Server {
	var options []p9net.ServerOpt
	if log != nil {
		options = []p9net.ServerOpt{
			p9net.WithServerLogger(log),
		}
	}
	return p9net.NewServer(fsys, options...)
}

func handleListeners(serveFn serveFunc,
	listeners <-chan manet.Listener, errs wgErrs,
	log ulog.Logger,
) {
	if log != nil &&
		log != ulog.Null {
		var listenersDuplicate,
			listenersLog <-chan manet.Listener
		relayUnordered(listeners,
			&listenersDuplicate, &listenersLog)
		listeners = listenersDuplicate
		go logListeners(log, listenersLog)
	}
	errs.Add(1)
	go serveListeners(serveFn, listeners, errs)
}

func serveListeners(serveFn serveFunc, listeners <-chan manet.Listener,
	errs wgErrs,
) {
	defer errs.Done()
	var (
		serveWg sync.WaitGroup
		serve   = func(listener manet.Listener) {
			defer serveWg.Done()
			err := serveFn(listener)
			if err == nil ||
				// Ignore value caused by server.Shutdown().
				// (daemon closed listener.)
				errors.Is(err, p9net.ErrServerClosed) ||
				// Ignore value caused by listener.Close().
				// (external|fs closed listener.)
				errors.Is(err, net.ErrClosed) {
				return
			}
			err = fmt.Errorf("%w: %s - %w",
				errServe, listener.Multiaddr(), err,
			)
			errs.send(err)
		}
	)
	for listener := range listeners {
		serveWg.Add(1)
		go serve(listener)
	}
	serveWg.Wait()
}

func handleStopSequence(ctx context.Context,
	server *p9net.Server, srvStop <-chan ShutdownDisposition,
	mount mountSubsystem, mntStop <-chan ShutdownDisposition,
	errs wgErrs, log ulog.Logger,
) *sync.WaitGroup {
	errs.Add(2)
	var serviceWg sync.WaitGroup
	serviceWg.Add(1)
	go func() {
		defer serviceWg.Done()
		serverStopper(ctx, server, srvStop, errs, log)
		unmountAll(mount, mntStop, errs, log)
	}()
	return &serviceWg
}

func serverStopper(ctx context.Context,
	server *p9net.Server, stopper <-chan ShutdownDisposition,
	errs wgErrs, log ulog.Logger,
) {
	defer errs.Done()
	const (
		deadline   = 10 * time.Second
		msgPrefix  = "stop signal received - "
		connPrefix = "closing connections"
		waitMsg    = msgPrefix + "closing listeners now" +
			" and connections when they're idle"
		nowMsg       = msgPrefix + connPrefix + " immediately"
		waitForConns = ShutdownPatient
		timeoutConns = ShutdownShort
		closeConns   = ShutdownImmediate
	)
	var (
		initiated    bool
		shutdownErr  = make(chan error, 1)
		sCtx, cancel = context.WithCancel(ctx)
	)
	defer cancel()
	for {
		select {
		case level, ok := <-stopper:
			if !ok {
				return
			}
			switch level {
			case waitForConns:
				log.Print(waitMsg)
			case timeoutConns:
				time.AfterFunc(deadline, cancel)
				log.Printf("%sforcefully %s in %s",
					msgPrefix, connPrefix, deadline)
			case closeConns:
				cancel()
				log.Print(nowMsg)
			default:
				err := fmt.Errorf("unexpected signal: %v", level)
				errs.send(err)
				continue
			}
			if !initiated {
				initiated = true
				go func() { shutdownErr <- server.Shutdown(sCtx) }()
			}
		case err := <-shutdownErr:
			if err != nil &&
				!errors.Is(err, context.Canceled) {
				errs.send(err)
			}
			return
		}
	}
}

func allServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	var (
		userMaddrs, uErr   = userServiceMaddrs()
		systemMaddrs, sErr = systemServiceMaddrs()
		serviceMaddrs      = append(userMaddrs, systemMaddrs...)
	)
	if err := errors.Join(uErr, sErr); err != nil {
		return nil, fmt.Errorf(
			"could not retrieve service maddrs: %w",
			err,
		)
	}
	return serviceMaddrs, nil
}

// TODO: [Ame] docs.
// userServiceMaddrs returns a list of multiaddrs that servers and client commands
// may try to use when hosting or querying a user-level file system service.
func userServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	return servicePathsToServiceMaddrs(xdg.StateHome, xdg.RuntimeDir)
}

// TODO: [Ame] docs.
// systemServiceMaddrs returns a list of multiaddrs that servers and client commands
// may try to use when hosting or querying a system-level file system service.
func systemServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	return hostServiceMaddrs()
}

func servicePathsToServiceMaddrs(servicePaths ...string) ([]multiaddr.Multiaddr, error) {
	var (
		serviceMaddrs = make([]multiaddr.Multiaddr, 0, len(servicePaths))
		multiaddrSet  = make(map[string]struct{}, len(servicePaths))
	)
	for _, servicePath := range servicePaths {
		if _, alreadySeen := multiaddrSet[servicePath]; alreadySeen {
			continue // Don't return duplicates in our slice.
		}
		multiaddrSet[servicePath] = struct{}{}
		var (
			nativePath        = filepath.Join(servicePath, ServiceName, ServerName)
			serviceMaddr, err = filepathToUnixMaddr(nativePath)
		)
		if err != nil {
			return nil, err
		}
		serviceMaddrs = append(serviceMaddrs, serviceMaddr)
	}
	return serviceMaddrs, nil
}

func filepathToUnixMaddr(nativePath string) (multiaddr.Multiaddr, error) {
	const (
		protocolPrefix = "/unix"
		unixNamespace  = len(protocolPrefix)
		slash          = 1
	)
	var (
		insertSlash = !strings.HasPrefix(nativePath, "/")
		size        = unixNamespace + len(nativePath)
	)
	if insertSlash {
		size += slash
	}
	// The component's protocol's value should be concatenated raw,
	// with platform native conventions. I.e. avoid [path.Join].
	// For non-Unix formatted filepaths, we'll need to insert the multiaddr delimiter.
	var maddrBuilder strings.Builder
	maddrBuilder.Grow(size)
	maddrBuilder.WriteString(protocolPrefix)
	if insertSlash {
		maddrBuilder.WriteRune('/')
	}
	maddrBuilder.WriteString(nativePath)
	return multiaddr.NewMultiaddr(maddrBuilder.String())
}
