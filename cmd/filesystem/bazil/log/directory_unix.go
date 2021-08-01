//go:build !nofuse && !(windows || plan9 || netbsd || openbsd)
// +build !nofuse,!windows,!plan9,!netbsd,!openbsd

package log

import (
	"context"

	fs "bazil.org/fuse/fs"
	logging "github.com/ipfs/go-log"
)

type Directory struct{ logging.EventLogger }

func (d *Directory) Lookup(_ context.Context, name string) (_ fs.Node, _ error) {
	d.Debugf("Directory Lookup: '%s'", name)
	return
}
