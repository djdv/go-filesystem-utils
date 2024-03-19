//go:build !darwin

package daemon

import (
	"github.com/adrg/xdg"
	"github.com/multiformats/go-multiaddr"
)

func hostServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	return servicePathsToServiceMaddrs(xdg.ConfigDirs...)
}
