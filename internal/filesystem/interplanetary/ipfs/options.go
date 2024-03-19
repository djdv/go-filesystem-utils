package ipfs

import (
	"context"
	"io/fs"
	"time"

	intp "github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/internal"
	"github.com/djdv/go-filesystem-utils/internal/generic"
)

type (
	settings struct {
		*FS
		defaultNodeCache,
		defaultDirCache bool
	}
	// Option allows changing default values used within [New].
	Option func(*settings) error
)

// Default values used by [New].
const (
	DefaultAPITimeout     = 1 * time.Minute
	DefaultPermissions    = intp.ReadAll | intp.ExecuteAll
	DefaultNodeCacheCount = 64
	DefaultDirCacheCount  = 64
	DefaultLinkLimit      = 40
)

// WithContext provides a parent context to use
// during operations that are cancellable.
func WithContext(ctx context.Context) Option {
	const name = "WithContext"
	return func(settings *settings) error {
		if err := generic.ErrIfOptionWasSet(
			name, settings.ctx, nil,
		); err != nil {
			return err
		}
		settings.ctx, settings.cancel = context.WithCancel(ctx)
		return nil
	}
}

// WithAPITimeout sets a timeout duration to use
// when communicating with the IPFS API/node.
// If <= 0, operations will not time out,
// and will remain pending until the file system is closed.
func WithAPITimeout(timeout time.Duration) Option {
	const name = "WithAPITimeout"
	return func(settings *settings) error {
		err := generic.ErrIfOptionWasSet(
			name, settings.apiTimeout, DefaultAPITimeout,
		)
		settings.apiTimeout = timeout
		return err
	}
}

// WithNodeCacheCount sets the number of IPLD nodes the
// file system will hold in its cache.
// If <= 0, caching of nodes is disabled.
func WithNodeCacheCount(cacheCount int) Option {
	const name = "WithNodeCaceCount"
	return func(settings *settings) error {
		if err := generic.ErrIfOptionWasSet(
			name, settings.defaultNodeCache, true,
		); err != nil {
			return err
		}
		settings.defaultNodeCache = false
		if cacheCount <= 0 {
			return nil
		}
		return settings.initNodeCache(cacheCount)
	}
}

// WithDirectoryCacheCount sets the number of directory
// entry-lists the file system will hold in its cache.
// If <= 0, caching of entries is disabled.
func WithDirectoryCacheCount(cacheCount int) Option {
	const name = "WithDirectoryCacheCount"
	return func(settings *settings) error {
		if err := generic.ErrIfOptionWasSet(
			name, settings.defaultDirCache, true,
		); err != nil {
			return err
		}
		settings.defaultDirCache = false
		if cacheCount <= 0 {
			return nil
		}
		return settings.initDirectoryCache(cacheCount)
	}
}

// WithLinkLimit sets the maximum amount of times an
// operation will resolve a symbolic link chain
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

// IPFS' UFS v1 does not store any permission data
// along with its files. As a result we apply blanket permissions
// to all files. This option sets what those permissions are.
func WithPermissions(permissions fs.FileMode) Option {
	const name = "WithPermissions"
	return func(settings *settings) error {
		if err := generic.ErrIfOptionWasSet(
			name, settings.info.Mode_.Perm(), DefaultPermissions,
		); err != nil {
			return err
		}
		return intp.SetModePermissions(&settings.info.Mode_, permissions)
	}
}
