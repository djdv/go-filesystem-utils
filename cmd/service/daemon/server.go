package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
	cmds "github.com/ipfs/go-ipfs-cmds"
	cmdshttp "github.com/ipfs/go-ipfs-cmds/http"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

const (
	// ServerRootName defines a name which servers and clients may use
	// to refer to the service in namespace oriented APIs.
	ServerRootName = "fs"

	// ServerName defines a name which servers and clients may use
	// to form or find connections to a named server instance.
	// (E.g. a Unix socket of path `.../$ServerRootName/$ServerName`.)
	ServerName = "server"
)

var ErrServiceNotFound = errors.New("could not find service instance")

// TODO: We should be consistent and return channel, not a slice.
// maybe errs instead of err, but err is probably fine. We expect these to vary
// by platform, but ultimately be static source data (for now).
//
// UserServiceMaddrs returns a list of multiaddrs that servers and client commands
// may try to use when hosting or querying a user-level file system service.
func UserServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	paths := []string{
		xdg.StateHome,
		xdg.RuntimeDir,
	}
	for i, path := range paths {
		paths[i] = filepath.Join(path, ServerRootName, ServerName)
	}
	return pathsToUnixMaddrs(paths...)
}

// TODO: We should be consistent and return channel, not a slice.
//
// SystemServiceMaddrs returns a list of multiaddrs that servers and client commands
// may try to use when hosting or querying a system-level file system service.
func SystemServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	return systemServiceMaddrs() // Platform specific.
}

func pathsToUnixMaddrs(servicePaths ...string) ([]multiaddr.Multiaddr, error) {
	serviceMaddrs := make([]multiaddr.Multiaddr, 0, len(servicePaths))
	for _, servicePath := range servicePaths {
		maddrString := path.Join("/unix/", servicePath)
		serviceMaddr, err := multiaddr.NewMultiaddr(maddrString)
		if err != nil {
			return nil, err
		}
		serviceMaddrs = append(serviceMaddrs, serviceMaddr)
	}
	return serviceMaddrs, nil
}

// FindLocalServer searches a set of local addresses
// and returns the first dialable maddr it finds.
// Otherwise it returns `ErrServiceNotFound`.
func FindLocalServer() (multiaddr.Multiaddr, error) {
	userMaddrs, err := UserServiceMaddrs()
	if err != nil {
		return nil, err
	}
	systemMaddrs, err := SystemServiceMaddrs()
	if err != nil {
		return nil, err
	}

	var (
		localDefaults = append(userMaddrs, systemMaddrs...)
		maddrStrings  = make([]string, len(localDefaults))
	)
	for i, serviceMaddr := range localDefaults {
		if ServerDialable(serviceMaddr) {
			return serviceMaddr, nil
		}
		maddrStrings[i] = serviceMaddr.String()
	}

	return nil, fmt.Errorf("%w: tried %s",
		ErrServiceNotFound, strings.Join(maddrStrings, ", "),
	)
}

// TODO: replace this.
// We should have something that both checks and returns a client
// By doing more than just dialing, some handshake protocol `/up`, `/version` etc.
// (so that we know we're talking to the daemon, not some arbitrary socket.)
//
// ServerDialable returns true if the multiaddr is dialable.
// Signifying the target service at that address is ready for operation.
func ServerDialable(maddr multiaddr.Multiaddr) (connected bool) {
	conn, err := manet.Dial(maddr)
	if err == nil && conn != nil {
		if err := conn.Close(); err != nil {
			return // Socket is faulty, not accepting.
		}
		connected = true
	}
	return
}

// TODO: unexport this in favour of something that
// makes the client and tests that the server is up?
// When would a caller need a client for a server that isn't up?
func MakeClient(maddr multiaddr.Multiaddr) (cmds.Executor, error) {
	clientHost, clientOpts, err := parseCmdsClientOptions(maddr)
	if err != nil {
		return nil, err
	}
	return cmdshttp.NewClient(clientHost, clientOpts...), nil
}

func parseCmdsClientOptions(maddr multiaddr.Multiaddr) (clientHost string, clientOpts []cmdshttp.ClientOpt, err error) {
	network, dialHost, err := manet.DialArgs(maddr)
	if err != nil {
		return "", nil, err
	}
	switch network {
	case "tcp", "tcp4", "tcp6":
		clientHost = dialHost
	case "unix":
		// TODO: Consider patching cmds-lib.
		// We want to use the URL scheme "http+unix".
		// But as-is, cmdslib doesn't recognize that protocol and will prefix the value
		// as `http://http+unix://`.
		// It would be nice if unix socket maddrs were handled internally by `cmdshttp`
		clientHost = fmt.Sprintf("http://%s-%s", ServerRootName, ServerName)
		netDialer := new(net.Dialer)
		clientOpts = append(clientOpts, cmdshttp.ClientWithHTTPClient(&http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return netDialer.DialContext(ctx, network, dialHost)
				},
			},
		}))
	default:
		return "", nil, fmt.Errorf("unsupported API address: %s", maddr)
	}
	return clientHost, clientOpts, nil
}
