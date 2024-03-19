//go:build !windows

package cgofuse

type sysquirks struct{}

func (*sysquirks) mountHook() { /* NOOP */ }

func (*sysquirks) unmountHook() { /* NOOP */ }
