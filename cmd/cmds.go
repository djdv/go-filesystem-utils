package fscmds

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/adrg/xdg"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

var ErrServiceNotFound = errors.New("could not find service instance")

// UserServiceMaddrs returns a list of multiaddrs that servers and client commands
// may try to use when hosting or querying a user-level file system service.
func UserServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	return servicePathsToServiceMaddrs(xdg.StateHome, xdg.RuntimeDir)
}

// SystemServiceMaddrs returns a list of multiaddrs that servers and client commands
// may try to use when hosting or querying a system-level file system service.
func SystemServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	if runtime.GOOS == "darwin" {
		return servicePathsToServiceMaddrs(
			"/Library/Application Support", // NeXT
			"/var/run",                     // BSD UNIX
		)
	} else {
		return servicePathsToServiceMaddrs(xdg.ConfigDirs...)
	}
}

func servicePathsToServiceMaddrs(servicePaths ...string) ([]multiaddr.Multiaddr, error) {
	var (
		serviceMaddrs = make([]multiaddr.Multiaddr, 0, len(servicePaths))
		multiaddrSet  = make(map[string]struct{}, len(servicePaths))
	)
	for _, servicePath := range servicePaths {
		if _, alreadySeen := multiaddrSet[servicePath]; alreadySeen {
			continue // Don't return duplicates in our slice.
		} else {
			multiaddrSet[servicePath] = struct{}{}
		}
		maddrString := path.Join("/unix/",
			filepath.Join(servicePath, ServiceName, ServerName))
		serviceMaddr, err := multiaddr.NewMultiaddr(maddrString)
		if err != nil {
			return nil, err
		}
		serviceMaddrs = append(serviceMaddrs, serviceMaddr)
	}
	return serviceMaddrs, nil
}

// FindLocalServer returns a local service socket's maddr,
// if an active server instance is found. Otherwise it returns
// `ErrServiceNotFound`.
func FindLocalServer() (multiaddr.Multiaddr, error) {
	userMaddrs, err := UserServiceMaddrs()
	if err != nil {
		return nil, err
	}
	systemMaddrs, err := SystemServiceMaddrs()
	if err != nil {
		return nil, err
	}
	localDefaults := append(userMaddrs, systemMaddrs...)

	for _, serviceMaddr := range localDefaults {
		if ClientDialable(serviceMaddr) {
			return serviceMaddr, nil
		}
	}

	// NOTE: We separate this loop out
	// so that it only allocates+executes if no servers are found.
	maddrStrings := make([]string, len(localDefaults))
	for i, serviceMaddr := range localDefaults {
		maddrStrings[i] = serviceMaddr.String()
	}

	return nil, fmt.Errorf("%w: tried %s", ErrServiceNotFound, strings.Join(maddrStrings, ", "))
}

// ClientDialable returns true if the multiaddr is dialable.
// Usually signifying the target service is ready for operation.
// Otherwise, it's down.
func ClientDialable(maddr multiaddr.Multiaddr) (connected bool) {
	socketPath, err := maddr.ValueForProtocol(multiaddr.P_UNIX)
	if err == nil {
		if runtime.GOOS == "windows" { // `/C:/path/...` -> `C:\path\...`
			socketPath = filepath.FromSlash(strings.TrimPrefix(socketPath, `/`))
		}
		fi, err := os.Lstat(socketPath)
		if err != nil {
			return false
		}

		// TODO: link issue tracker number
		// FIXME: [2021.04.30 / Go 1.16]
		// Go does not set socket mode on Windows
		// change this when resolved
		if runtime.GOOS != "windows" {
			return fi.Mode()&os.ModeSocket != 0
		}
		// HACK:
		// for now, try dialing the socket
		// but only when it exists, otherwise we'd create it
		// and need to clean that up
	}
	conn, err := manet.Dial(maddr)
	if err == nil && conn != nil {
		if err = conn.Close(); err != nil {
			return // socket is faulty, not accepting.
		}
		connected = true
	}
	return
}
