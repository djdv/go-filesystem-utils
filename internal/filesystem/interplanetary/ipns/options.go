package ipns

import (
	"context"
	"io/fs"
	"time"

	intp "github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/internal"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/ipfs"
	"github.com/djdv/go-filesystem-utils/internal/generic"
)

type (
	settings struct {
		*FS
		optionConflict   string
		ipfsOptions      []ipfs.Option
		defaultRootCache bool
	}
	// Option allows changing default values used within [New].
	Option func(*settings) error
)

// Default values used by [New].
const (
	DefaultAPITimeout  = ipfs.DefaultAPITimeout
	DefaultPermissions = ipfs.DefaultPermissions
	DefaultCacheExpiry = 1 * time.Minute
	DefaultLinkLimit   = ipfs.DefaultLinkLimit
)

// WithIPFS provides an existing IPFS instance.
// If not provided, a new instance is instantiated during [New].
func WithIPFS(ipfs fs.FS) Option {
	const name = "WithIPFS"
	return func(settings *settings) error {
		if err := generic.ErrIfOptionWasSet(
			name, settings.ipfs, nil,
		); err != nil {
			return err
		}
		if conflict := settings.optionConflict; conflict != "" {
			return conflictErr(name, conflict)
		}
		settings.optionConflict = name
		settings.ipfs = ipfs
		return nil
	}
}

// WithIPFSOptions provides options that are passed to
// [IPFS.New] inside our own [New].
func WithIPFSOptions(options []ipfs.Option) Option {
	const name = "WithIPFSOptions"
	return func(settings *settings) error {
		if settings.ipfsOptions != nil {
			return generic.OptionAlreadySet(name)
		}
		if conflict := settings.optionConflict; conflict != "" {
			return conflictErr(name, conflict)
		}
		settings.optionConflict = name
		settings.ipfsOptions = options
		return nil
	}
}

// WithPermissions sets the permissions of the [FS] root.
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

// WithRootCache sets the number of root names to cache.
// Roots will be resolved and held in the cache until they expire
// or this count is exceeded.
// If <=0, caching of names is disabled.
func WithRootCache(cacheCount int) Option {
	const name = "WithRootCache"
	return func(set *settings) error {
		if err := generic.ErrIfOptionWasSet(
			name, set.defaultRootCache, true,
		); err != nil {
			return err
		}
		set.defaultRootCache = false
		if cacheCount <= 0 {
			return nil
		}
		return set.initRootCache(cacheCount)
	}
}

// CacheNodesFor sets how long a node is considered
// valid within the cache. After this time, the node
// will be refreshed during its next operation.
func CacheNodesFor(duration time.Duration) Option {
	const name = "CacheNodesFor"
	return func(set *settings) error {
		err := generic.ErrIfOptionWasSet(
			name, set.expiry, DefaultCacheExpiry,
		)
		set.expiry = duration
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
