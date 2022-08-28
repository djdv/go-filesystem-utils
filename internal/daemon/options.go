package daemon

import (
	"github.com/adrg/xdg"
	"github.com/multiformats/go-multiaddr"
	"github.com/u-root/uio/ulog"
)

type (
	ClientOption func(*Client) error

	// TODO: server options removed, remove this too?
	NetOptions interface{ ClientOption }
)

const (
	// TODO: del
	// ServiceMaddr is the default multiaddr used by servers and clients.
	// ServiceMaddr = "/ip4/127.0.0.1/tcp/564"

	// TODO: [Ame] docs.
	// ServerRootName defines a name which servers and clients may use
	// to refer to the service in namespace oriented APIs.
	ServerRootName = "fs"

	// TODO: [Ame] docs.
	// ServerName defines a name which servers and clients may use
	// to form or find connections to a named server instance.
	// (E.g. a Unix socket of path `.../$ServerRootName/$ServerName`.)
	ServerName = "server"
)

// TODO: [Ame] docs.
// UserServiceMaddrs returns a list of multiaddrs that servers and client commands
// may try to use when hosting or querying a user-level file system service.
func UserServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	return servicePathsToServiceMaddrs(xdg.StateHome, xdg.RuntimeDir)
}

// TODO: [Ame] docs.
// SystemServiceMaddrs returns a list of multiaddrs that servers and client commands
// may try to use when hosting or querying a system-level file system service.
func SystemServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	return systemServiceMaddrs() // Platform specific.
}

func AllServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	var (
		userMaddrs, uErr   = UserServiceMaddrs()
		systemMaddrs, sErr = SystemServiceMaddrs()
		serviceMaddrs      = append(userMaddrs, systemMaddrs...)
	)
	for _, e := range []error{uErr, sErr} {
		if e != nil {
			return nil, e
		}
	}
	return serviceMaddrs, nil
}

func WithLogger[OT NetOptions](log ulog.Logger) (option OT) {
	switch fnPtrPtr := any(&option).(type) {
	case *ClientOption:
		*fnPtrPtr = func(c *Client) error { c.log = log; return nil }
	}
	return option
}

func WithIPFS(maddr multiaddr.Multiaddr) MountOption {
	return func(s *mountSettings) error { s.ipfs.nodeMaddr = maddr; return nil }
}
