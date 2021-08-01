package cgofuse

import (
	"fmt"
	"os"

	"github.com/ipfs/go-ipfs/filesystem"
)

func ParseRequest(sysID filesystem.ID, target string) (request Request, err error) {
	// basic Info
	request.HostPath = target
	opts := fmt.Sprintf("fsname=%s,subtype=%s",
		sysID.String(),
		sysID.String(),
	)

	// TODO: [general] we should allow the user to pass in raw options
	// that we will then relay to the underlying fuse implementation, unaltered
	// options like `allow_other` depend on opinions of the sysop, not us
	// so we shouldn't just assume this is what they want
	if os.Geteuid() == 0 { // if root, allow other users to access the mount
		opts += ",allow_other" // allow users besides root to see and access the mount

		//opts += ",default_permissions"
		// TODO: [cli, constructors]
		// for now, `default_permissions` won't prevent anything
		// since we tell whoever is calling that they own the file, regardless of who it is
		// we need a way for the user to set `uid` and `gid` values
		// both for our internal context (getattr)
		// as well as allowing them to pass the uid= and gid= FUSE options (not specifically, pass anything)
		// (^system ignores our values and substitutes its own)
	}

	request.FuseArgs = append(request.FuseArgs, "-o", opts)

	return
}
