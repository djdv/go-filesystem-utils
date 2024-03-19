package nfs

import "github.com/djdv/go-filesystem-utils/internal/generic"

type (
	settings struct {
		cacheLimit int
	}
	Option func(*settings) error
)

const DefaultCacheLimit = 1024

func WithCacheLimit(limit int) Option {
	const name = "WithCacheLimit"
	return func(settings *settings) error {
		err := generic.ErrIfOptionWasSet(
			name, settings.cacheLimit, DefaultCacheLimit,
		)
		settings.cacheLimit = limit
		return err
	}
}
