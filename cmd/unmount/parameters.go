package unmount

import (
	"context"

	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type Settings struct {
	settings.Settings
	settings.UnmountSettings
}

func (self *Settings) Parameters(ctx context.Context) parameters.Parameters {
	return CtxMerge(ctx,
		(*settings.Settings)(nil).Parameters(ctx),
		(*settings.UnmountSettings)(nil).Parameters(ctx),
	)
}
