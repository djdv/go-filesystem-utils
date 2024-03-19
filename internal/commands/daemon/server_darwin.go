package daemon

import "github.com/multiformats/go-multiaddr"

func hostServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	return servicePathsToServiceMaddrs(
		"/Library/Application Support", // NeXT
		"/var/run",                     // BSD UNIX
	)
}
