package nfs

import (
	"errors"
	"io"
	"io/fs"
	"net"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/go-git/go-billy/v5/helper/polyfill"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/willscott/go-nfs"
	nfshelper "github.com/willscott/go-nfs/helpers"
)

func Mount(maddr multiaddr.Multiaddr, fsys fs.FS, options ...Option) (io.Closer, error) {
	settings := settings{
		cacheLimit: DefaultCacheLimit,
	}
	if err := generic.ApplyOptions(&settings, options...); err != nil {
		return nil, err
	}
	listener, err := manet.Listen(maddr)
	if err != nil {
		return nil, err
	}
	// The NFS library has verbose logging by default.
	// If the operator has not specified a log level,
	// override the library's default level.
	// (Primarily to suppress `ENOENT` errors in the console.)
	const nfslibEnvKey = "LOG_LEVEL"
	if _, set := os.LookupEnv(nfslibEnvKey); !set {
		nfs.Log.SetLevel(nfs.PanicLevel)
	}
	var (
		server        = &server{fs: fsys}
		billyFsys     = polyfill.New(server)
		nfsHandler    = nfshelper.NewNullAuthHandler(billyFsys)
		cacheLimit    = settings.cacheLimit
		cachedHandler = nfshelper.NewCachingHandler(nfsHandler, cacheLimit)
		goListener    = manet.NetListener(listener)
		errsCh        = make(chan error, 1)
	)
	go func() { errsCh <- nfs.Serve(goListener, cachedHandler) }()
	return generic.Closer(func() error {
		if err := listener.Close(); err != nil {
			return err
		}
		if err := <-errsCh; !errors.Is(err, net.ErrClosed) {
			return err
		}
		return nil
	}), nil
}
