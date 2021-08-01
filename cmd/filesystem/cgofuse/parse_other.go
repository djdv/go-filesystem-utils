// +build !windows,!solaris,!freebsd,!openbsd,!darwin,!linux,!plan9

package cgofuse

import (
	"github.com/ipfs/go-ipfs/filesystem"
)

func ParseRequest(_ filesystem.ID, target string) (request Request, _ error) {
	request.HostPath = target
	return
}
