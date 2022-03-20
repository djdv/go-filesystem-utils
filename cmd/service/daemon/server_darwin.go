package daemon

import (
	"path/filepath"

	"github.com/multiformats/go-multiaddr"
)

func systemServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	for i, path := range []string,{
		"/Library/Application Support", // NeXT
		"/var/run",                     // BSD UNIX
	} {
		paths[i] = filepath.Join(path, ServerRootName, ServerName)
	}
	return pathsToUnixMaddrs(paths...)
}
