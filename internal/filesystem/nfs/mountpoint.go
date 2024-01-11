package nfs

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	maddrc "github.com/djdv/go-filesystem-utils/internal/multiaddr"
	"github.com/go-git/go-billy/v5/helper/polyfill"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/willscott/go-nfs"
	nfshelper "github.com/willscott/go-nfs/helpers"
)

type (
	// Host holds metadata required to host
	// a file system as an NFS server.
	Host struct {
		Maddr multiaddr.Multiaddr `json:"maddr,omitempty"`
	}
)

const HostID filesystem.Host = "NFS"

func (*Host) HostID() filesystem.Host { return HostID }

func (nh *Host) UnmarshalJSON(b []byte) error {
	// multiformats/go-multiaddr issue #100
	var maddrWorkaround struct {
		Maddr maddrc.Multiaddr `json:"maddr,omitempty"`
	}
	if err := json.Unmarshal(b, &maddrWorkaround); err != nil {
		return err
	}
	nh.Maddr = maddrWorkaround.Maddr.Multiaddr
	return nil
}

func (nh *Host) Mount(fsys fs.FS) (io.Closer, error) {
	listener, err := manet.Listen(nh.Maddr)
	if err != nil {
		return nil, err
	}
	const cacheLimit = 1024
	var (
		netFsys                      = &netFS{fsys: fsys}
		billyFsys                    = polyfill.New(netFsys)
		nfsHandler                   = nfshelper.NewNullAuthHandler(billyFsys)
		cachedHandler                = nfshelper.NewCachingHandler(nfsHandler, cacheLimit)
		goListener                   = manet.NetListener(listener)
		errsCh                       = make(chan error, 1)
		closerFn      generic.Closer = func() error {
			if err := listener.Close(); err != nil {
				return err
			}
			if err := <-errsCh; !errors.Is(err, net.ErrClosed) {
				return err
			}
			return nil
		}
	)
	// The NFS library has verbose logging by default.
	// If the operator has not specified a log level,
	// override the library's default level.
	// (Primarily to suppress `ENOENT` errors in the console.)
	const nfslibEnvKey = "LOG_LEVEL"
	if _, set := os.LookupEnv(nfslibEnvKey); !set {
		nfs.Log.SetLevel(nfs.PanicLevel)
	}
	go func() { errsCh <- nfs.Serve(goListener, cachedHandler) }()
	return closerFn, nil
}
