//go:build !nofuse && !(windows || plan9 || netbsd || openbsd)
// +build !nofuse,!windows,!plan9,!netbsd,!openbsd

package ipns

import (
	"context"
	"os"

	"bazil.org/fuse"
	logging "github.com/ipfs/go-log"
)

type Link struct {
	Target string
	log    logging.EventLogger
}

func (l *Link) Attr(ctx context.Context, a *fuse.Attr) error {
	l.log.Debug("Link attr.")
	a.Mode = os.ModeSymlink | 0555
	return nil
}

func (l *Link) Readlink(ctx context.Context, req *fuse.ReadlinkRequest) (string, error) {
	l.log.Debugf("ReadLink: %s", l.Target)
	return l.Target, nil
}
