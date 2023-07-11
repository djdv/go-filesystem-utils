//go:build !windows

package commands

import "github.com/multiformats/go-multiaddr"

func createServiceMaddrs() ([]multiaddr.Multiaddr, cleanupFunc, error) {
	serviceMaddrs, err := hostServiceMaddrs()
	if err != nil {
		return nil, nil, err
	}
	return serviceMaddrs[:1], nil, nil
}
