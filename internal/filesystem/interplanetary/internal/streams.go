package interplanetary

import (
	"context"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

type (
	ctxChan[T any] struct {
		context.Context
		context.CancelFunc
		Ch <-chan T
	}
	EntryStream = ctxChan[filesystem.StreamDirEntry]
)
