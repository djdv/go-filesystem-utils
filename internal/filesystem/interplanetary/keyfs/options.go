package keyfs

import (
	"context"
	"io/fs"
	"time"

	intp "github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/internal"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/ipfs"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	coreiface "github.com/ipfs/boxo/coreiface"
)

// Option allows changing default values used within [New].
type Option func(*FS) error

// Default values used by [New].
const (
	DefaultAPITimeout  = ipfs.DefaultAPITimeout
	DefaultPermissions = intp.ReadAll | intp.ExecuteAll
	DefaultCacheExpiry = 30 * time.Second
	DefaultLinkLimit   = ipfs.DefaultLinkLimit
)

// WithIPNS supplies an IPNS instance to
// use for added functionality.
// Such as resolving files that a key references.
func WithIPNS(ipns fs.FS) Option {
	const name = "WithIPNS"
	return func(fsys *FS) error {
		err := generic.ErrIfOptionWasSet(
			name, fsys.ipns, nil,
		)
		fsys.ipns = ipns
		return err
	}
}

// WithNameService supplies the name service for added functionality.
// Such as creating files.
func WithNameService(names coreiface.NameAPI) Option {
	const name = "WithNameService"
	return func(fsys *FS) error {
		err := generic.ErrIfOptionWasSet(
			name, fsys.names, nil,
		)
		fsys.names = names
		return err
	}
}

func WithDagService(dag coreiface.APIDagService) Option {
	const name = "WithDagService"
	return func(fsys *FS) error {
		err := generic.ErrIfOptionWasSet(
			name, fsys.dag, nil,
		)
		fsys.dag = dag
		return err
	}
}

// WithPinService supplies the pin service for added functionality.
// Such as pinning files which are created.
func WithPinService(pins coreiface.PinAPI) Option {
	const name = "WithPinService"
	return func(fsys *FS) error {
		err := generic.ErrIfOptionWasSet(
			name, fsys.pins, nil,
		)
		fsys.pins = pins
		return err
	}
}

// CacheKeysFor will cache responses from the node and consider
// them valid for the duration. Negative values retain the
// cache forever. A 0 value disables caching.
func CacheKeysFor(duration time.Duration) Option {
	const name = "CacheKeysFor"
	return func(fsys *FS) error {
		err := generic.ErrIfOptionWasSet(
			name, fsys.expiry, DefaultCacheExpiry,
		)
		fsys.expiry = duration
		return err
	}
}

// WithContext provides a parent context to use
// during operations that are cancellable.
func WithContext(ctx context.Context) Option {
	const name = "WithContext"
	return func(fsys *FS) error {
		if err := generic.ErrIfOptionWasSet(
			name, fsys.ctx, nil,
		); err != nil {
			return err
		}
		fsys.ctx, fsys.cancel = context.WithCancel(ctx)
		return nil
	}
}

// WithPermissions sets the permissions of the [FS] root.
func WithPermissions(permissions fs.FileMode) Option {
	const name = "WithPermissions"
	return func(fsys *FS) error {
		if err := generic.ErrIfOptionWasSet(
			name, fsys.info.Mode_.Perm(), DefaultPermissions,
		); err != nil {
			return err
		}
		return intp.SetModePermissions(&fsys.info.Mode_, permissions)
	}
}

// WithAPITimeout sets a timeout duration to use
// when communicating with the IPFS API/node.
// If <= 0, operations will not time out,
// and will remain pending until the file system is closed.
func WithAPITimeout(timeout time.Duration) Option {
	const name = "WithAPITimeout"
	return func(fsys *FS) error {
		err := generic.ErrIfOptionWasSet(
			name, fsys.apiTimeout, DefaultAPITimeout,
		)
		fsys.apiTimeout = timeout
		return err
	}
}

// WithLinkLimit sets the maximum amount of times an
// operation will resolve a symbolic link chain,
// before it returns a recursion error.
func WithLinkLimit(limit uint) Option {
	const name = "WithLinkLimit"
	return func(fsys *FS) error {
		err := generic.ErrIfOptionWasSet(
			name, fsys.linkLimit, DefaultLinkLimit,
		)
		fsys.linkLimit = limit
		return err
	}
}
