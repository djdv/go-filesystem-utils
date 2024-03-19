package cgofuse

import (
	"time"
)

type sysquirks struct {
	remounting bool
}

func (sq *sysquirks) mountHook() {
	// Issue: [upstream] cgofuse / WinFSP.
	// Calling `unmount();mount()` will almost
	// always fail, with the OS claiming the mount
	// point is still in use.
	// Unfortunately the suggested workaround is
	// to wait an arbitrary amount of time for the
	// system to actually release the resource.
	// It would be better is we could receive some signal
	// either here, or patched upstream to prevent `unmount`
	// from returning after making the request, but before
	// the request has been fulfilled (by the OS).
	if sq.remounting {
		time.Sleep(128 * time.Millisecond)
	}
}

func (sq *sysquirks) unmountHook() {
	sq.remounting = true
}
