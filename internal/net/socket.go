package net

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	deferMutex struct{ sync.Mutex }

	ListenerManager struct {
		activeMu deferMutex
		active   listenersMap
	}
	listenersMap = map[manet.Listener]*ConnectionManager

	serverHandleFunc = func(io.ReadCloser, io.WriteCloser) error
)

// TODO: [7dd5513d-4991-46c9-8632-fc36475e88a8]
// TODO: better name?
// TODO: investigate impact; sugar is not worth costs in this context, but this might be free.
func (dm *deferMutex) locks() func() { dm.Lock(); return dm.Unlock }

func (lm *ListenerManager) exists(listener manet.Listener) bool {
	_, ok := lm.active[listener]
	return ok
}

func (lm *ListenerManager) Add(listener manet.Listener) (*ConnectionManager, error) {
	defer lm.activeMu.locks()()
	if lm.exists(listener) {
		return nil, fmt.Errorf("%s was already added", listener)
	}
	active := lm.active
	if active == nil {
		active = make(listenersMap)
		lm.active = active
	}
	conns := new(ConnectionManager)
	active[listener] = conns
	return conns, nil
}

func (lm *ListenerManager) Remove(listener manet.Listener) error {
	defer lm.activeMu.locks()()
	if !lm.exists(listener) {
		return fmt.Errorf("%s was not previously added", listener)
	}
	if delete(lm.active, listener); len(lm.active) == 0 {
		lm.active = nil
	}
	return nil
}

func (lm *ListenerManager) Shutdown(ctx context.Context) error {
	defer lm.activeMu.locks()()
	connectionManagers, err := closeAllListeners(lm.active)
	if err != nil {
		return err
	}
	connMans := make(map[*ConnectionManager]struct{}, len(connectionManagers))
	for _, connMan := range connectionManagers {
		connMans[connMan] = struct{}{}
	}
	var errs []error
	for len(connMans) != 0 {
		for connMan := range connMans {
			connMan.activeMu.Lock()
			if len(connMan.active) == 0 {
				connMan.activeMu.Unlock()
				delete(connMans, connMan)
				continue
			}
			var closeConns func(connectionsMap) error
			if ctx.Err() != nil {
				closeConns = closeAllConns
			} else {
				closeConns = closeIdle
			}
			cErr := closeConns(connMan.active)
			connMan.activeMu.Unlock()
			if cErr != nil {
				errs = append(errs, err)
				delete(connMans, connMan)
				continue
			}
			if len(connMan.active) != 0 {
				time.Sleep(1 * time.Second) // TODO: const
			}
		}
	}
	return joinErrs(errs...)
}

func closeAllListeners(listeners listenersMap) ([]*ConnectionManager, error) {
	var (
		errs     []error
		connMans = make([]*ConnectionManager, 0, len(listeners))
	)
	for listener, connectionManager := range listeners {
		if err := listener.Close(); err != nil {
			errs = append(errs, err)
		}
		delete(listeners, listener)
		connMans = append(connMans, connectionManager)
	}
	if errs != nil {
		return nil, joinErrs(errs...)
	}
	return connMans, nil
}
