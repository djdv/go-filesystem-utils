package net

import (
	"fmt"
	"time"

	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	trackedConn struct {
		lastRead time.Time
		manet.Conn
	}

	connectionsMap    = map[manet.Conn]*time.Time
	ConnectionManager struct {
		activeMu deferMutex
		active   connectionsMap
	}
)

func (tc *trackedConn) Read(b []byte) (int, error) {
	tc.lastRead = time.Now()
	return tc.Conn.Read(b)
}

func (cm *ConnectionManager) exists(conn manet.Conn) bool {
	_, ok := cm.active[conn]
	return ok
}

func (cm *ConnectionManager) Add(conn manet.Conn) (manet.Conn, error) {
	defer cm.activeMu.locks()()
	if cm.exists(conn) {
		return nil, fmt.Errorf("%s was already added", conn)
	}
	active := cm.active
	if active == nil {
		active = make(connectionsMap)
		cm.active = active
	}
	tc := &trackedConn{
		lastRead: time.Now(),
		Conn:     conn,
	}
	active[conn] = &tc.lastRead
	return tc, nil
}

func (cm *ConnectionManager) Remove(conn manet.Conn) {
	defer cm.activeMu.locks()()
	delete(cm.active, conn)
}

func closeIdle(conns connectionsMap) error {
	const threshold = 30 * time.Second
	var (
		now  = time.Now()
		errs []error
	)
	// TODO: filter list, then send to closeAll instead of dupe logic
	for connection, lastActive := range conns {
		if now.Sub(*lastActive) >= threshold {
			delete(conns, connection)
			if err := connection.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return joinErrs(errs...)
}

func closeAllConns(conns connectionsMap) error {
	var errs []error
	for connection := range conns {
		delete(conns, connection)
		if err := connection.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return joinErrs(errs...)
}

func joinErrs(errs ...error) (err error) {
	for _, e := range errs {
		if err == nil {
			err = e
		} else {
			err = fmt.Errorf("%w\n%s", err, e)
		}
	}
	return
}
