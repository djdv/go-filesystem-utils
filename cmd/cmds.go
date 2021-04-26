package fscmds

import (
	"errors"
	"fmt"
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
	}
	return servicePathsToServiceMaddrs(xdg.ConfigDirs...)
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

// ServerDialable returns true if the multiaddr is dialable.
// Signifying the target service at that address is ready for operation.
func ServerDialable(maddr multiaddr.Multiaddr) (connected bool) {
	conn, err := manet.Dial(maddr)
	if err == nil && conn != nil {
		if err = conn.Close(); err != nil {
			return // Socket is faulty, not accepting.
		}
		connected = true
	}
	return
}
