//go:build !windows

package cgofuse

type sysquirks struct{}

func (*sysquirks) mount() { /* NOOP */ }

func (*sysquirks) unmount() { /* NOOP */ }
