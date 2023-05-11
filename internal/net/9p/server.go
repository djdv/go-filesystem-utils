// Package p9 adds a shutdown method to a [p9.Server].
package p9

import (
	"context"
	"errors"
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
		log          ulog.Logger
		server       *p9.Server
		connections  connectionMap
		listeners    listenerMap
		listenersWg  sync.WaitGroup
		idleDuration time.Duration
		mu           sync.Mutex
		shutdown     atomic.Bool
	}
	// TrackedIO exposes metrics around an IO interface.
	TrackedIO interface {
		LastRead() time.Time
		LastWrite() time.Time
		io.ReadWriteCloser
	}
	trackedReads interface {
		io.ReadCloser
		LastRead() time.Time
	}
	trackedWrites interface {
		io.WriteCloser
		LastWrite() time.Time
	}
	trackedIOpair struct {
		trackedReads
		trackedWrites
	}
	postCloseFunc     = func()
	trackedReadCloser struct {
		trackedReads
		postCloseFn postCloseFunc
	}
	trackedWriteCloser struct {
		trackedWrites
		postCloseFn postCloseFunc
	}
	// The same notes in [net/http]'s pkg apply to us.
	// Specifically; interfaces as keys will panic
	// if the underlying type is unhashable;
	// thus the pointer-to-interface.
	listenerMap   map[*manet.Listener]struct{}
	connectionMap map[*trackedIOpair]struct{}
	manetConn     = manet.Conn
	// TrackedConn records metrics
	// of a network connection.
	TrackedConn struct {
		read, wrote *atomic.Pointer[time.Time]
		manetConn
	}
	trackedReader struct {
		last *atomic.Pointer[time.Time]
		io.ReadCloser
	}
	trackedWriter struct {
		last *atomic.Pointer[time.Time]
		io.WriteCloser
	}
	// onceCloseListener wraps a net.Listener, protecting it from
	// multiple Close calls. (Specifically in Serve; Close; Shutdown)
	onceCloseListener struct {
		manet.Listener
		*onceCloser
	}
	// onceCloseIO wraps an [io.ReadWriteCloser],
	// protecting it from multiple Close calls.
	// This is necessary before passing to
	// [p9.Server.Handle] (which implicitly calls close
	// on both its arguments).
	onceCloseIO struct {
		io.ReadWriteCloser
		*onceCloser
	}
	onceCloseTrackedIO struct {
		TrackedIO
		*onceCloser
	}
	onceCloser struct {
		error
		sync.Once
	}
)

// ErrServerClosed may be returned by [Server.Serve] methods
// after [Server.Shutdown] or [Server.Close] is called.
const ErrServerClosed generic.ConstError = "p9: Server closed"

// NewServer wraps the
// [p9.NewServer] constructor.
func NewServer(attacher p9.Attacher, options ...ServerOpt) *Server {
	const defaultIdleDuration = 30 * time.Second
	var (
		passthrough []p9.ServerOpt
		srv         = Server{
			log:          ulog.Null,
			idleDuration: defaultIdleDuration,
		}
	)
	for _, applyAndUnwrap := range options {
		if relayedOpt := applyAndUnwrap(&srv); relayedOpt != nil {
			passthrough = append(passthrough, relayedOpt)
		}
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

// WithIdleDuration sets the duration used by the server
// when evaluating connection idleness.
// If the time since the last connection operation
// exceeds the duration, it will be considered idle.
func WithIdleDuration(d time.Duration) ServerOpt {
	return func(s *Server) p9.ServerOpt {
		s.idleDuration = d
		return nil
	}
}

// Handle handles a single connection.
// If [TrackedIO] is passed in for either or both
// of the transmit and receive parameters, they will be
// asserted and re-used. This allows the [Server] and caller
// to share metrics without requiring extra overhead.
func (srv *Server) Handle(t io.ReadCloser, r io.WriteCloser) error {
	var (
		trackedT, trackedR = makeTrackedIO(t, r)
		connection         = &trackedIOpair{
			trackedReads:  trackedT,
			trackedWrites: trackedR,
		}
		connections             = srv.getConnections()
		closedRead, closedWrite bool
		deleteFn                = func() {
			srv.mu.Lock()
			defer srv.mu.Unlock()
			delete(connections, connection)
		}
		cleanupT = trackedReadCloser{
			trackedReads: trackedT,
			postCloseFn: func() {
				closedRead = true
				if closedWrite {
					deleteFn()
				}
			},
		}
		cleanupR = trackedWriteCloser{
			trackedWrites: trackedR,
			postCloseFn: func() {
				closedWrite = true
				if closedRead {
					deleteFn()
				}
			},
		}
	)
	srv.mu.Lock()
	connections[connection] = struct{}{}
	srv.mu.Unlock()
	// HACK: Despite having valid value methods,
	// we pass an address because the 9P server
	// uses the `%p` verb in its log's format string.
	return srv.server.Handle(&cleanupT, &cleanupR)
}

func makeTrackedIO(rc io.ReadCloser, wc io.WriteCloser) (trackedReads, trackedWrites) {
	var (
		trackedR, rOk = rc.(trackedReads)
		trackedW, wOK = wc.(trackedWrites)
		needTimestamp = !rOk || !wOK
		stamp         *time.Time
	)
	if needTimestamp {
		now := time.Now()
		stamp = &now
	}
	if !rOk {
		var (
			ptr     atomic.Pointer[time.Time]
			tracked = trackedReader{
				last:       &ptr,
				ReadCloser: rc,
			}
		)
		ptr.Store(stamp)
		trackedR = tracked
	}
	if !wOK {
		var (
			ptr     atomic.Pointer[time.Time]
			tracked = trackedWriter{
				last:        &ptr,
				WriteCloser: wc,
			}
		)
		ptr.Store(stamp)
		trackedW = tracked
	}
	return trackedR, trackedW
}

func (srv *Server) getConnections() connectionMap {
	if connections := srv.connections; connections != nil {
		return connections
	}
	connections := make(connectionMap)
	srv.connections = connections
	return connections
}

// Serve handles requests from the listener.
//
// The passed listener _must_ be created in packet mode.
func (srv *Server) Serve(listener manet.Listener) (err error) {
	listener = onceCloseListener{
		Listener:   listener,
		onceCloser: new(onceCloser),
	}
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
			return err
		}
		go func(t io.ReadCloser, r io.WriteCloser) {
			// If a connection fails, we'll just alert the operator.
			// No need to accumulate these, nor take the whole server down.
			if err := handle(t, r); err != nil &&
				err != io.EOF {
				if srv.shuttingDown() &&
					errors.Is(err, net.ErrClosed) {
					return // Shutdown expected, drop error.
				}
				srv.log.Printf("connection handler encountered an error: %s\n", err)
			}
		}(splitConn(connection))
	}
}

func splitConn(connection manet.Conn) (io.ReadCloser, io.WriteCloser) {
	if tracked, ok := connection.(TrackedIO); ok {
		closeConnOnce := onceCloseTrackedIO{
			TrackedIO:  tracked,
			onceCloser: new(onceCloser),
		}
		return closeConnOnce, closeConnOnce
	}
	closeConnOnce := onceCloseIO{
		ReadWriteCloser: connection,
		onceCloser:      new(onceCloser),
	}
	return closeConnOnce, closeConnOnce
}

func (srv *Server) shuttingDown() bool {
	return srv.shutdown.Load()
}

func (srv *Server) trackListener(listener manet.Listener) (*manet.Listener, error) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
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
	srv.mu.Lock()
	defer srv.mu.Unlock()
	delete(srv.listeners, listener)
	srv.listenersWg.Done()
}

// Close requests the server to stop serving immediately.
// Listeners and connections associated with the server
// become closed by this call.
func (srv *Server) Close() error {
	srv.shutdown.Store(true)
	srv.mu.Lock()
	defer srv.mu.Unlock()
	err := srv.closeListenersLocked()
	// NOTE: refer to [net/http.Server]
	// implementation for lock sequence explanation.
	srv.mu.Unlock()
	srv.listenersWg.Wait()
	srv.mu.Lock()
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

// Shutdown requests the server to stop accepting new request
// and eventually close.
// Listeners associated with the server become closed immediately,
// and connections become closed when they are considered idle.
// If the context is done, connections become closed immediately.
func (srv *Server) Shutdown(ctx context.Context) error {
	srv.shutdown.Store(true)
	srv.mu.Lock()
	err := srv.closeListenersLocked()
	// NOTE: refer to [net/http.Server]
	// implementation for lock sequence explanation.
	srv.mu.Unlock()
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
			srv.mu.Lock()
			defer srv.mu.Unlock()
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
	threshold := srv.idleDuration
	allIdle = true
	srv.mu.Lock()
	defer srv.mu.Unlock()
	for connection := range srv.connections {
		var (
			lastActive = lastActive(connection)
			isIdle     = time.Since(lastActive) >= threshold
		)
		if !isIdle {
			allIdle = false
			continue
		}
		if cErr := connection.Close(); cErr != nil {
			err = fserrors.Join(err, cErr)
		}
		delete(srv.connections, connection)
	}
	return allIdle, err
}

func (srv *Server) closeAllConns() (err error) {
	for connection := range srv.connections {
		if cErr := (*connection).Close(); cErr != nil {
			err = fserrors.Join(err, cErr)
		}
	}
	return err
}

// NewTrackedConn wraps conn, providing operation metrics.
func NewTrackedConn(conn manet.Conn) TrackedConn {
	var (
		now         = time.Now()
		nowAddr     = &now
		read, wrote atomic.Pointer[time.Time]
		tracked     = TrackedConn{
			read:      &read,
			wrote:     &wrote,
			manetConn: conn,
		}
	)
	read.Store(nowAddr)
	wrote.Store(nowAddr)
	return tracked
}

// Read performs a read operation and updates the
// operation timestamp if successful.
func (tc TrackedConn) Read(b []byte) (int, error) {
	return trackRead(tc.manetConn, tc.read, b)
}

// LastRead returns the timestamp of the last successful read.
func (tc TrackedConn) LastRead() time.Time {
	return *tc.read.Load()
}

// Write performs a write operation and updates the
// operation timestamp if successful.
func (tc TrackedConn) Write(b []byte) (int, error) {
	return trackWrite(tc.manetConn, tc.wrote, b)
}

// LastWrite returns the timestamp of the last successful write.
func (tc TrackedConn) LastWrite() time.Time {
	return *tc.wrote.Load()
}

// Close closes the connection.
func (tc TrackedConn) Close() error {
	return tc.manetConn.Close()
}

func (tr trackedReader) Read(b []byte) (int, error) {
	return trackRead(tr.ReadCloser, tr.last, b)
}

func (tr trackedReader) LastRead() time.Time {
	return *tr.last.Load()
}

func (tw trackedWriter) Write(b []byte) (int, error) {
	return trackWrite(tw.WriteCloser, tw.last, b)
}

func (tw trackedWriter) LastWrite() time.Time {
	return *tw.last.Load()
}

func (ol onceCloseListener) Close() error {
	ol.Once.Do(func() { ol.error = ol.Listener.Close() })
	return ol.error
}

func (oc onceCloseIO) Close() error {
	oc.Once.Do(func() { oc.error = oc.ReadWriteCloser.Close() })
	return oc.error
}

func (oc onceCloseTrackedIO) Close() error {
	oc.Once.Do(func() { oc.error = oc.TrackedIO.Close() })
	return oc.error
}

func trackRead(r io.Reader, stamp *atomic.Pointer[time.Time], b []byte) (int, error) {
	read, err := r.Read(b)
	if err != nil {
		return read, err
	}
	now := time.Now()
	stamp.Store(&now)
	return read, nil
}

func trackWrite(w io.Writer, stamp *atomic.Pointer[time.Time], b []byte) (int, error) {
	wrote, err := w.Write(b)
	if err != nil {
		return wrote, err
	}
	now := time.Now()
	stamp.Store(&now)
	return wrote, nil
}

func lastActive(tio TrackedIO) time.Time {
	var (
		read  = tio.LastRead()
		write = tio.LastWrite()
	)
	if read.After(write) {
		return read
	}
	return write
}

func (ct *trackedIOpair) Close() error {
	return fserrors.Join(
		ct.trackedReads.Close(),
		ct.trackedWrites.Close(),
	)
}

func (trc trackedReadCloser) Close() error {
	err := trc.trackedReads.Close()
	trc.postCloseFn()
	return err
}

func (twc trackedWriteCloser) Close() error {
	err := twc.trackedWrites.Close()
	twc.postCloseFn()
	return err
}
