package mount

import (
	"context"

	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type Settings struct {
	settings.Settings
	settings.MountSettings
}

func (self *Settings) Parameters(ctx context.Context) parameters.Parameters {
	return CtxJoin(ctx,
		(*settings.Settings).Parameters(nil, ctx),
		(*settings.MountSettings).Parameters(nil, ctx),
	)
}
