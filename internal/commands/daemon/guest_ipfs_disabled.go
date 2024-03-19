//go:build noipfs

package daemon

import "github.com/djdv/go-filesystem-utils/internal/filesystem"

func makeIPFSGuests[
	hostI hostPtr[host],
	host any,
](filesystem.Host, mountPointGuests, ninePath) { /* NOOP */
}
