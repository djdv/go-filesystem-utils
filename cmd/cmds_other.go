//go:build !darwin
// +build !darwin

package fscmds

import (
	"github.com/adrg/xdg"
	"github.com/multiformats/go-multiaddr"
)

func systemServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	return servicePathsToServiceMaddrs(xdg.ConfigDirs...)
}
