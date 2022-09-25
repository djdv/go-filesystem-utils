package daemon

import (
	"github.com/adrg/xdg"
	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
	"github.com/u-root/uio/ulog"
)

const (
	idLength       = 9
	base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
)

type (
	ClientOption func(*Client) error

	// TODO: these should be shared literally
	// I.e. 9lib.Mount and client.Mount should use the same option type/structs
	MountOption   func(*mountSettings) error
	mountSettings struct {
		ipfs struct {
			nodeMaddr multiaddr.Multiaddr
		}
		uid p9.UID
		gid p9.GID
		/*
			fuse struct {
				// fsid  filesystem.ID
				// fsapi filesystem.API
				uid, gid uint32
			}
		*/
	}

	UnmountOption   func(*unmountSettings) error
	unmountSettings struct {
		all bool
	}
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

func WithLogger(log ulog.Logger) ClientOption {
	return func(c *Client) error { c.log = log; return nil }
}

func WithIPFS(maddr multiaddr.Multiaddr) MountOption {
	return func(s *mountSettings) error { s.ipfs.nodeMaddr = maddr; return nil }
}

// TODO: shared option?
func UnmountAll(b bool) UnmountOption {
	return func(us *unmountSettings) error { us.all = b; return nil }
}
