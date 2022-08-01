package daemon

import (
	"github.com/hugelgupf/p9/p9"
	"github.com/u-root/uio/ulog"
)

type (
	ServerOption func(*Server) error
	ClientOption func(*Client) error

	SharedOptions interface{ ServerOption | ClientOption }
)

// ServiceMaddr is the default multiaddr used by servers and clients.
const ServiceMaddr = "/ip4/127.0.0.1/tcp/564"

func WithLogger[OT SharedOptions](log ulog.Logger) (option OT) {
	switch fnPtrPtr := any(&option).(type) {
	case *ServerOption:
		*fnPtrPtr = func(s *Server) error { s.log = log; return nil }
	case *ClientOption:
		*fnPtrPtr = func(c *Client) error { c.log = log; return nil }
	}
	return option
}

func WithUID(uid p9.UID) ServerOption { return func(s *Server) error { s.uid = uid; return nil } }
func WithGID(gid p9.GID) ServerOption { return func(s *Server) error { s.gid = gid; return nil } }
