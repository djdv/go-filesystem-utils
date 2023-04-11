// Package p9 adds a shutdown method to a [p9.Server].
package p9

import (
	"context"
	"io"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/hugelgupf/p9/p9"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/u-root/uio/ulog"
)

type (
	// NOTE: unfortunately we need to mock all the upstream options
	// if we want to hijack the logger for our own use.

	// ServerOpt is an optional config for a new server.
	ServerOpt func(s *Server) p9.ServerOpt

	// Server adds Close and Shutdown methods
	// similar to [net/http.Server], for a [p9.Server].
	Server struct {
		shutdown    atomic.Bool
		log         ulog.Logger
		server      *p9.Server
		listeners   listenerMap
		connections connectionMap
		sync.Mutex
		listenersWg sync.WaitGroup
	}
	// The same notes in [net/http]'s pkg apply to us.
	// Specifically; interfaces as keys will panic
	// if the underlying type is unhashable;
	// thus the rare pointer-to-interface.
	listenerMap   map[*manet.Listener]struct{}
	connectionMap map[*trackedIO]struct{}

	trackedIO struct {
		last
		io.ReadCloser
		io.WriteCloser
		dropSelf func(*trackedIO)
	}
	last struct {
		read, write time.Time
	}

	// onceCloseListener wraps a net.Listener, protecting it from
	// multiple Close calls. (Specifically in Serve; Close; Shutdown)
	onceCloseListener struct {
		manet.Listener
		onceCloser
	}
	// onceCloseConn wraps a [net.Conn], protecting it from
	// multiple Close calls.
	// This is necessary before passing to
	// [p9.Server.Handle] (which implicitly calls close
	// on both its arguments).
	onceCloseConn struct {
		net.Conn
		onceCloser
	}

	onceCloser struct {
		error
		sync.Once
	}
	// TODO: Cross pkg witchcraft.
	// We need to export this interface so that pkgs can be
	// aware of it. Specifically, p9fs' listenerFile should
	// implement this but only import it via its test pkg.
	// Alternatively, we could consider giving listenerFile
	// a callback which halts Serve (<- this seems less good)
	fsListener interface {
		// TODO: this name should probably be changed to be more general
		// Graceful, Closed, or something.
		Unlinked() bool
	}
)

const ErrServerClosed generic.ConstError = "p9: Server closed"

// NewServer simply wraps the native
// [p9.NewServer] constructor.
func NewServer(attacher p9.Attacher, options ...ServerOpt) *Server {
	var (
		passthrough = make([]p9.ServerOpt, len(options))
		srv         = Server{
			log: ulog.Null,
		}
	)
	for i, applyAndUnwrap := range options {
		passthrough[i] = applyAndUnwrap(&srv)
	}
	srv.server = p9.NewServer(attacher, passthrough...)
	return &srv
}

// WithServerLogger overrides the default logger for the server.
func WithServerLogger(l ulog.Logger) ServerOpt {
	return func(s *Server) p9.ServerOpt {
		s.log = l
		return p9.WithServerLogger(l)
	}
}

// Handle handles a single connection.
func (srv *Server) Handle(t io.ReadCloser, r io.WriteCloser) error {
	tracked := srv.trackIO(t, r)
	return srv.server.Handle(tracked, tracked)
}

// Serve handles requests from the listener.
//
// The passed listener _must_ be created in packet mode.
func (srv *Server) Serve(listener manet.Listener) (err error) {
	listener = &onceCloseListener{Listener: listener}
	trackToken, err := srv.trackListener(listener)
	if err != nil {
		return err
	}
	defer func() {
		err = fserrors.Join(err, listener.Close())
		srv.dropListener(trackToken)
	}()
	for handle := srv.Handle; ; {
		connection, err := listener.Accept()
		if err != nil {
			if srv.shuttingDown() {
				return ErrServerClosed
			}
			if fsListener, ok := listener.(fsListener); ok {
				// Listener was closed gracefully
				// by some external means.
				if fsListener.Unlinked() {
					return ErrServerClosed
				}
			}
			return err
		}
		closeConnOnce := &onceCloseConn{
			Conn: connection,
		}
		go func() {
			// If a connection fails, we'll just alert the operator.
			// No need to accumulate these, nor take the whole server down.
			if err := handle(closeConnOnce, closeConnOnce); err != nil &&
				err != io.EOF {
				srv.log.Printf("connection handler encountered an error: %s\n", err)
			}
		}()
	}
}

func (srv *Server) shuttingDown() bool {
	return srv.shutdown.Load()
}

func (srv *Server) trackListener(listener manet.Listener) (*manet.Listener, error) {
	srv.Mutex.Lock()
	defer srv.Mutex.Unlock()
	if srv.shuttingDown() {
		return nil, ErrServerClosed
	}
	var (
		listeners = srv.listeners
		lPtr      = &listener
	)
	if listeners == nil {
		listeners = make(listenerMap, 1)
		srv.listeners = listeners
	}
	listeners[lPtr] = struct{}{}
	srv.listenersWg.Add(1)
	return lPtr, nil
}

func (srv *Server) dropListener(listener *manet.Listener) {
	srv.Lock()
	defer srv.Unlock()
	delete(srv.listeners, listener)
	srv.listenersWg.Done()
}

func (srv *Server) trackIO(rc io.ReadCloser, wc io.WriteCloser) *trackedIO {
	srv.Mutex.Lock()
	defer srv.Mutex.Unlock()
	var (
		now     = time.Now()
		tracked = &trackedIO{
			last: last{
				read: now, write: now,
			},
			ReadCloser:  rc,
			WriteCloser: wc,
			dropSelf:    srv.dropIO,
		}
		connections = srv.connections
	)
	if connections == nil {
		connections = make(connectionMap, 1)
		srv.connections = connections
	}
	connections[tracked] = struct{}{}
	return tracked
}

func (srv *Server) dropIO(tio *trackedIO) {
	srv.Mutex.Lock()
	defer srv.Mutex.Unlock()
	delete(srv.connections, tio)
}

func (srv *Server) Close() error {
	srv.shutdown.Store(true)
	srv.Mutex.Lock()
	defer srv.Mutex.Unlock()
	err := srv.closeListenersLocked()
	// NOTE: refer to [net/http.Server]
	// implementation for lock sequence explanation.
	srv.Mutex.Unlock()
	srv.listenersWg.Wait()
	srv.Mutex.Lock()
	return fserrors.Join(err, srv.closeAllConns())
}

func (srv *Server) closeListenersLocked() error {
	var err error
	for listener := range srv.listeners {
		if cErr := (*listener).Close(); cErr != nil {
			err = fserrors.Join(err, cErr)
		}
	}
	return err
}

func (srv *Server) Shutdown(ctx context.Context) error {
	srv.shutdown.Store(true)
	srv.Mutex.Lock()
	err := srv.closeListenersLocked()
	// NOTE: refer to [net/http.Server]
	// implementation for lock sequence explanation.
	srv.Mutex.Unlock()
	srv.listenersWg.Wait()
	var (
		nextPollInterval = makeJitterFunc(time.Millisecond)
		timer            = time.NewTimer(nextPollInterval())
	)
	defer timer.Stop()
	for {
		idle, iErr := srv.closeIdleConns()
		if iErr != nil {
			err = fserrors.Join(err, iErr)
		}
		if idle {
			return err
		}
		select {
		case <-ctx.Done():
			return fserrors.Join(err,
				srv.closeAllConns(),
				ctx.Err(),
			)
		case <-timer.C:
			timer.Reset(nextPollInterval())
		}
	}
}

func makeJitterFunc(initial time.Duration) func() time.Duration {
	// Adapted from an inlined [net/http] closure.
	const pollIntervalMax = 500 * time.Millisecond
	return func() time.Duration {
		// Add 10% jitter.
		interval := initial +
			time.Duration(rand.Intn(int(initial/10)))
		// Double and clamp for next time.
		initial *= 2
		if initial > pollIntervalMax {
			initial = pollIntervalMax
		}
		return interval
	}
}

func (srv *Server) closeIdleConns() (allIdle bool, err error) {
	const threshold = 30 * time.Second
	allIdle = true
	for connection := range srv.connections {
		var (
			lastActive = connection.last.active()
			isIdle     = time.Since(lastActive) >= threshold
		)
		if !isIdle {
			allIdle = false
			continue
		}
		if cErr := connection.Close(); cErr != nil {
			err = fserrors.Join(err, cErr)
		}
	}
	return allIdle, err
}

func (srv *Server) closeAllConns() (err error) {
	for connection := range srv.connections {
		if cErr := connection.Close(); cErr != nil {
			err = fserrors.Join(err, cErr)
		}
	}
	return err
}

func (tio *trackedIO) Read(b []byte) (int, error) {
	tio.read = time.Now()
	return tio.ReadCloser.Read(b)
}

func (tio *trackedIO) Write(b []byte) (int, error) {
	tio.write = time.Now()
	return tio.WriteCloser.Write(b)
}

func (tio *trackedIO) Close() error {
	defer tio.dropSelf(tio)
	return fserrors.Join(tio.ReadCloser.Close(),
		tio.WriteCloser.Close())
}

func (l *last) active() time.Time {
	var (
		read  = l.read
		write = l.write
	)
	if read.After(write) {
		return read
	}
	return write
}

func (ol *onceCloseListener) Close() error {
	ol.Once.Do(func() {
		ol.error = ol.Listener.Close()
	})
	return ol.error
}

func (oc *onceCloseConn) Close() error {
	oc.Once.Do(func() {
		oc.error = oc.Conn.Close()
	})
	return oc.error
}
