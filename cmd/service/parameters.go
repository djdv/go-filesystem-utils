package service

import (
	"context"

	"github.com/djdv/go-filesystem-utils/cmd/service/host"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type (
	Host     = host.Settings
	Settings struct {
		settings.Settings
		Host
	}
)

func (*Settings) Parameters(ctx context.Context) parameters.Parameters {
	return CtxJoin(ctx,
		(*settings.Settings).Parameters(nil, ctx),
		(*host.Settings).Parameters(nil, ctx),
	)
}
