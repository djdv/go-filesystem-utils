package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	deferMutex struct{ sync.Mutex }

	listenersMap    = map[manet.Listener]*connectionManager
	listenerManager struct {
		activeMu deferMutex
		active   listenersMap
	}

	connectionsMap    = map[manet.Conn]*time.Time
	connectionManager struct {
		activeMu deferMutex
		active   connectionsMap
	}

	trackedConn struct {
		manet.Conn
		*time.Time
	}

	serverHandleFunc = func(io.ReadCloser, io.WriteCloser) error
)

// TODO: better name?
// TODO: investigate impact; sugar is not worth costs in this context, but this might be free.
func (dm *deferMutex) locks() func() { dm.Lock(); return dm.Unlock }

// TODO: review names <-> functionality. I think some of these are deceptive.
// I.e. "new" might actually upsert in some versions of these functions.

func (ls *listenerManager) new(listener manet.Listener) *connectionManager {
	defer ls.activeMu.locks()()

	active := ls.active
	if active == nil {
		active = make(listenersMap)
		ls.active = active
	}
	conns := new(connectionManager)
	active[listener] = conns

	return conns
}

func (ls *listenerManager) delete(listener manet.Listener) {
	defer ls.activeMu.locks()()
	delete(ls.active, listener)
}

func (cm *connectionManager) new(conn manet.Conn) *time.Time {
	defer cm.activeMu.locks()()
	active := cm.active
	if active == nil {
		active = make(connectionsMap)
		cm.active = active
	}
	var (
		now        = time.Now()
		lastActive = &now
	)
	active[conn] = lastActive
	return lastActive
}

func (cm *connectionManager) delete(conn manet.Conn) {
	defer cm.activeMu.locks()()
	delete(cm.active, conn)
}

func (tc *trackedConn) Read(b []byte) (int, error) { *tc.Time = time.Now(); return tc.Conn.Read(b) }

func closeIdle(conns connectionsMap) error {
	const threshold = 30 * time.Second
	var (
		now  = time.Now()
		errs []error
	)
	for connection, lastActive := range conns {
		if now.Sub(*lastActive) >= threshold {
			delete(conns, connection) // XXX: Review sync.
			if err := connection.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return joinErrs(errs...)
}

func closeAll(conns connectionsMap) error {
	var errs []error
	for connection := range conns {
		delete(conns, connection) // XXX: Review sync.
		if err := connection.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return joinErrs(errs...)
}

func serve(ctx context.Context,
	listener manet.Listener, listMan *listenerManager,
	handle serverHandleFunc,
) <-chan error {
	errs := make(chan error)
	go func() {
		defer close(errs)
		var (
			connectionsWg     sync.WaitGroup
			connMan           = listMan.new(listener)
			acceptCtx, cancel = context.WithCancel(ctx)
			conns, acceptErrs = accept(acceptCtx, listener)
		)
		defer cancel()
		defer listMan.delete(listener)
		for connOrErr := range generic.CtxEither(acceptCtx, conns, acceptErrs) {
			var (
				conn = connOrErr.Left
				err  = connOrErr.Right
			)
			if err != nil {
				select {
				case errs <- err:
					continue
				case <-ctx.Done():
					return
				}
			}
			connectionsWg.Add(1)
			go func(cn manet.Conn) {
				defer connectionsWg.Done()
				defer connMan.delete(cn)
				tc := &trackedConn{
					Conn: cn,
					Time: connMan.new(conn),
				}
				if err := handle(tc, tc); err != nil {
					if !errors.Is(err, io.EOF) {
						select {
						case errs <- err:
						case <-ctx.Done():
						}
					}
				}
			}(conn)
		}
		connectionsWg.Wait()
	}()
	return errs
}

func accept(ctx context.Context, listener manet.Listener) (<-chan manet.Conn, <-chan error) {
	var (
		conns = make(chan manet.Conn)
		errs  = make(chan error)
	)
	go func() {
		defer close(conns)
		defer close(errs)
		for {
			conn, err := listener.Accept()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					select {
					case errs <- err:
					case <-ctx.Done():
					}
				}
				return
			}
			select {
			case conns <- conn:
			case <-ctx.Done():
				conn.Close()
				return
			}
		}
	}()
	return conns, errs
}

func shutdown(ctx context.Context, listMan *listenerManager) error {
	listenerMap := listMan.active
	defer listMan.activeMu.locks()()
	var err error
	for listener := range listenerMap {
		if cErr := listener.Close(); cErr != nil {
			if err == nil {
				err = cErr
			} else {
				err = fmt.Errorf("%w\n\t%s", err, cErr)
			}
		}
	}
	if err != nil {
		return err
	}
	for len(listenerMap) != 0 {
		for listener, connections := range listenerMap {
			connections.activeMu.Lock()
			if len(connections.active) == 0 {
				delete(listenerMap, listener)
				connections.activeMu.Unlock()
				continue
			}
			var closeConns func(connectionsMap) error
			if ctx.Err() != nil {
				closeConns = closeAll
			} else {
				closeConns = closeIdle
			}
			cErr := closeConns(connections.active)
			connections.activeMu.Unlock()
			if cErr != nil {
				return cErr
			}
			if len(connections.active) != 0 {
				// TODO: const
				time.Sleep(1 * time.Second)
			}
		}
	}
	return nil
}
