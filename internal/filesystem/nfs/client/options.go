package nfs

import (
	"os"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/willscott/go-nfs-client/nfs"
	"github.com/willscott/go-nfs-client/nfs/rpc"
)

type (
	settings struct {
		*FS
		hostname, dirpath  string
		uid, gid           uint32
		uidValid, gidValid bool
	}
	Option func(*settings) error
)

// Default values used by [New].
const (
	DefaultLinkLimit     = 40
	DefaultLinkSeparator = "/"
	DefaultDirpath       = "/"
)

func (settings *settings) fillDefaults() error {
	if settings.hostname == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return err
		}
		settings.hostname = hostname
	}
	// TODO: should we default to "overflow" ID instead of current?
	// On systems with /proc; check `/proc/sys/kernel/overflowuid`
	// fallback to `^uint16(0)-1`.
	if !settings.uidValid {
		settings.uid = uint32(os.Getuid())
	}
	if !settings.gidValid {
		settings.gid = uint32(os.Getgid())
	}
	return nil
}

func (settings *settings) nfsTarget(maddr multiaddr.Multiaddr) (*nfs.Target, error) {
	naddr, err := manet.ToNetAddr(maddr)
	if err != nil {
		return nil, err
	}
	const (
		network    = "tcp"
		privledged = false
	)
	client, err := rpc.DialTCP(network, naddr.String(), privledged)
	if err != nil {
		return nil, err
	}
	var (
		auth = rpc.NewAuthUnix(
			settings.hostname,
			settings.uid, settings.gid,
		).Auth()
		mounter = nfs.Mount{Client: client}
		dirpath = settings.dirpath
	)
	target, err := mounter.Mount(dirpath, auth)
	if err != nil {
		mounter.Close()
		return nil, err
	}
	return target, nil
}

// WithUID overrides the default NFS `uid` value.
// Used in the `AUTH_UNIX` authentication "flavor".
func WithUID(uid uint32) Option {
	const name = "WithUID"
	return func(settings *settings) error {
		err := generic.ErrIfOptionWasSet(
			name, settings.uidValid, false,
		)
		settings.uid = uid
		settings.uidValid = true
		return err
	}
}

// WithGID overrides the default NFS `gid` value.
// Used in the `AUTH_UNIX` authentication "flavor".
func WithGID(gid uint32) Option {
	const name = "WithGID"
	return func(settings *settings) error {
		err := generic.ErrIfOptionWasSet(
			name, settings.gidValid, false,
		)
		settings.gid = gid
		settings.gidValid = true
		return err
	}
}

// WithDirpath overrides the default NFS `dirpath` value.
// Specifies the path on the NFS server to be mounted.
func WithDirpath(path string) Option {
	const name = "WithDirpath"
	return func(settings *settings) error {
		err := generic.ErrIfOptionWasSet(
			name, settings.dirpath, DefaultDirpath,
		)
		settings.dirpath = path
		return err
	}
}

// WithHostname overrides the default NFS `hostname` value.
// Used in the `AUTH_UNIX` authentication "flavor".
func WithHostname(hostname string) Option {
	const name = "WithHostname"
	return func(settings *settings) error {
		err := generic.ErrIfOptionWasSet(
			name, settings.hostname, "",
		)
		settings.hostname = hostname
		return err
	}
}

// WithLinkSeparator sets a string to be used when normalizing
// symbolic link targets during internal file system operations
// (ReadLink is unaffected).
// E.g. consider a link target `target\with slash`, by default the system
// interprets that as a single file whose name contains a `\`.
// If the link separator is set to `\`, then the link is converted to
// `target/with slash`, where `name` is now internally considered a directory.
// You'd want to use this if the NFS server is hosting links with relative
// targets that are formatted in the DOS/NT (or other) convention.
func WithLinkSeparator(separator string) Option {
	const name = "WithLinkSeparator"
	return func(settings *settings) error {
		err := generic.ErrIfOptionWasSet(
			name, settings.linkSeparator, DefaultLinkSeparator,
		)
		settings.linkSeparator = separator
		return err
	}
}

// WithLinkLimit sets the maximum amount of times an
// operation will resolve a symbolic link chain,
// before it returns a recursion error.
func WithLinkLimit(limit uint) Option {
	const name = "WithLinkLimit"
	return func(settings *settings) error {
		err := generic.ErrIfOptionWasSet(
			name, settings.linkLimit, DefaultLinkLimit,
		)
		settings.linkLimit = limit
		return err
	}
}
