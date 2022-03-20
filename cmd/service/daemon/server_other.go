//go:build !darwin
// +build !darwin

package daemon

import (
	"path/filepath"

	"github.com/adrg/xdg"
	"github.com/multiformats/go-multiaddr"
)

func systemServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	paths := make([]string, len(xdg.ConfigDirs))
	copy(paths, xdg.ConfigDirs)
	for i, path := range paths {
		paths[i] = filepath.Join(path, ServerRootName, ServerName)
	}
	return pathsToUnixMaddrs(paths...)
}
