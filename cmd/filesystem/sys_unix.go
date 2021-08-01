//go:build !windows
// +build !windows

package fscmds

import "github.com/multiformats/go-multiaddr"

// xdg already creates these for us
func makeServiceDir(multiaddr.Multiaddr) error { return nil }
