package files

import (
	"sync/atomic"

	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
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
	DirectoryOption func(*Directory) error

	ListenerOption   func(*Listener) error
	listenerOption   func(*listenerFile) error
	ipfsTargetOption func(*ipfsTarget) error

	// NOTE: [go/issues/48522] is on the milestone for 1.20,
	// but was punted from 1.19 so maybe|maybe-not.
	// If it happens, implementers of this can be simplified dramatically.
	// For now lots of type boilerplate is needed,
	// and it's going to be easy to forget to add types to all shared opts. :^(
	//
	// TODO better name
	// We can unexport this too but it's kind of rude and ugly for the decls that need it.
	SharedOptions interface {
		DirectoryOptions | FileOptions
	}
	DirectoryOptions interface {
		DirectoryOption | ListenerOption
	}
	FileOptions interface {
		listenerOption | ipfsTargetOption
	}
)

func WithPath[OT SharedOptions](path *atomic.Uint64) (option OT) {
	switch fnPtrPtr := any(&option).(type) {
	case *DirectoryOption:
		*fnPtrPtr = func(dir *Directory) error { dir.path = path; return nil }
	case *ListenerOption:
		*fnPtrPtr = func(ld *Listener) error { ld.path = path; return nil }
	case *listenerOption:
		*fnPtrPtr = func(lf *listenerFile) error { lf.path = path; return nil }
	case *ipfsTargetOption:
		*fnPtrPtr = func(it *ipfsTarget) error { it.path = path; return nil }
	}
	return option
}

func WithParent[OT SharedOptions](parent p9.File) (option OT) {
	switch fnPtrPtr := any(&option).(type) {
	case *DirectoryOption:
		*fnPtrPtr = func(dir *Directory) error { dir.parentFile = parent; return nil }
	case *ListenerOption:
		*fnPtrPtr = func(ld *Listener) error { ld.parentFile = parent; return nil }
	case *listenerOption:
		*fnPtrPtr = func(lf *listenerFile) error { lf.parentFile = parent; return nil }
	case *ipfsTargetOption:
		*fnPtrPtr = func(it *ipfsTarget) error { it.parentFile = parent; return nil }
	}
	return option
}

// TODO: name: WithMknodCallback?
func WithCallback(cb ListenerCallback) ListenerOption {
	return func(l *Listener) error { l.mknodCallback = cb; return nil }
}

func withPrefix(prefix multiaddr.Multiaddr) ListenerOption {
	return func(l *Listener) error { l.prefix = prefix; return nil }
}
func withProtocol(protocol string) ListenerOption {
	return func(l *Listener) error { l.protocol = protocol; return nil }
}
