package daemon

import (
	"context"
	"time"

	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/p9/p9"
)

func newListener(ctx context.Context, parent p9.File, path ninePath,
	uid p9.UID, gid p9.GID, permissions p9.FileMode,
) (listenSubsystem, error) {
	lCtx, cancel := context.WithCancel(ctx)
	_, listenFS, listeners, err := p9fs.NewListener(lCtx,
		p9fs.WithParent[p9fs.ListenerOption](parent, ListenersFileName),
		p9fs.WithPath[p9fs.ListenerOption](path),
		p9fs.WithUID[p9fs.ListenerOption](uid),
		p9fs.WithGID[p9fs.ListenerOption](gid),
		p9fs.WithPermissions[p9fs.ListenerOption](permissions),
		p9fs.UnlinkEmptyChildren[p9fs.ListenerOption](true),
	)
	if err != nil {
		cancel()
		return listenSubsystem{}, err
	}
	return listenSubsystem{
		name:      ListenersFileName,
		Listener:  listenFS,
		listeners: listeners,
		cancel:    cancel,
	}, nil
}

func hasActiveClients(listeners p9.File, threshold time.Duration) (bool, error) {
	infos, err := p9fs.GetConnections(listeners)
	if err != nil {
		return false, err
	}
	for _, info := range infos {
		lastActive := lastActive(&info)
		if time.Since(lastActive) <= threshold {
			return true, nil
		}
	}
	return false, nil
}

func lastActive(info *p9fs.ConnInfo) time.Time {
	var (
		read  = info.LastRead
		write = info.LastWrite
	)
	if read.After(write) {
		return read
	}
	return write
}
