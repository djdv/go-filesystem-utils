//go:build !nofuse && !(windows || plan9 || netbsd || openbsd)
// +build !nofuse,!windows,!plan9,!netbsd,!openbsd

package log

import (
	"context"

	fuse "bazil.org/fuse"
	fs "bazil.org/fuse/fs"
	logging "github.com/ipfs/go-log"
)

type (
	Node struct{ logging.EventLogger }
	File struct{ logging.EventLogger }
)

func (n *Node) Attr(_ context.Context, _ *fuse.Attr) (_ error) { n.Debug("Node Attr"); return }
func (n *Node) Lookup(_ context.Context, name string) (_ fs.Node, _ error) {
	n.Debugf("Node Lookup: '%s'", name)
	return
}
