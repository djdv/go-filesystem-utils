package pinfs

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
	DefaultPermissions = ipfs.DefaultPermissions
	DefaultCacheExpiry = 30 * time.Second
)

// WithIPFS supplies an IPFS instance to
// use for added functionality.
// One such case is resolving a pin's file metadata.
func WithIPFS(ipfs fs.FS) Option {
	const name = "WithIPFS"
	return func(fsys *FS) error {
		err := generic.ErrIfOptionWasSet(
			name, fsys.ipfs, nil,
		)
		fsys.ipfs = ipfs
		return err
	}
}

// WithDagService supplies a dag service to
// use to add support for various write operations.
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

// CachePinsFor will cache responses from the node and consider
// them valid for the duration. Negative values retain the
// cache forever. A 0 value disables caching.
func CachePinsFor(duration time.Duration) Option {
	const name = "CachePinsFor"
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

// WithPermissions sets the permissions of the [FS] root.
func WithPermissions(permissions fs.FileMode) Option {
	const name = "WithPermissions"
	return func(fsys *FS) error {
		if err := generic.ErrIfOptionWasSet(
			name, fsys.info.permissions, DefaultPermissions,
		); err != nil {
			return err
		}
		return intp.SetModePermissions(&fsys.info.permissions, permissions)
	}
}
