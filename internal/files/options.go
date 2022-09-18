package files

import (
	"sync/atomic"
)

// TODO: some way to provide statfs for files that are themselves,
// not devices, but hosted inside one.
//
// Implementations should probably have a default of `0x01021997` (V9FS_MAGIC) for `f_type`
// Or we can make up our own magic numbers (something not already in use)
// to guarantee we're not misinterpreted (as a FS that we're not)
// by callers / the OS (Linux specifically).
//
// The Linux manual has this to say about `f_fsid`
// "Nobody knows what f_fsid is supposed to contain" ...
// we'll uhhh... figure something out later I guess.

type (
	MetaOption func(*metadata) error

	// ListenerOption func(*Listener) error
	// listenerOption   func(*listenerFile) error
	ipfsTargetOption func(*ipfsTarget) error
)

func WithPath(path *atomic.Uint64) MetaOption {
	return func(m *metadata) error { m.path = path; return nil }
}

/*
func WithParent(parent p9.File) MetaOption {
	return func(m *metadata) error { m.parentFile = parent; return nil }
}
*/

// TODO: name: WithMknodCallback?
/*
func WithCallback(cb ListenerCallback) ListenerOption {
	return func(l *Listener) error { l.mknodCallback = cb; return nil }
}
*/

/*
func withPrefix(prefix multiaddr.Multiaddr) ListenerOption {
	return func(l *Listener) error { l.prefix = prefix; return nil }
}

func withProtocol(protocol string) ListenerOption {
	return func(l *Listener) error { l.protocol = protocol; return nil }
}
*/
